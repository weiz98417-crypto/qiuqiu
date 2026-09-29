# -*- coding: utf-8 -*-
"""FunASR 2pass 评估 runner(asr-selfhost-eval 9.1)。

口径:
- partial 首 token:前缀探针(prefix-probe)。对流式模型(paraformer-zh-streaming,
  chunk_size=[0,10,5] = 600ms 主块)从 100ms 起逐档截取前缀喂入,记录"出现非空
  partial 所需的最短音频时长(audioLeadMs)"与"该次前缀的本地计算耗时(computeMs)"。
  感知首响 = audioLeadMs + computeMs。该口径同时施加于 MiMo 侧(其 audioLead 固定
  1500ms 窗 + 网络 RTT),保证可比。
- final:seaco-paraformer(短名 paraformer-zh)同一批音频跑两组:无热词 vs 热词
  (热词=主路 voiceRecognitionHints 同源全表,生产同形)。文本写盘,CER 由 node 侧
  lib/textnorm.mjs 统一计算。

用法:python run-funasr.py --audio-dir <dir> --cases <cases.json> --out <results.json>
"""

import argparse
import json
import os
import sys
import time
import wave


def read_wav(path):
    with wave.open(path, "rb") as w:
        assert w.getframerate() == 16000, f"{path}: expect 16k, got {w.getframerate()}"
        assert w.getsampwidth() == 2, f"{path}: expect pcm16"
        frames = w.readframes(w.getnframes())
    return frames


def make_wav(pcm, rate=16000):
    import io
    import struct

    buf = io.BytesIO()
    with wave.open(buf, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(rate)
        w.writeframes(pcm)
    return buf.getvalue()


def result_text(res):
    if isinstance(res, list) and res:
        return (res[0].get("text") or "").strip()
    if isinstance(res, dict):
        return (res.get("text") or "").strip()
    return ""


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--audio-dir", required=True)
    parser.add_argument("--cases", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument(
        "--prefixes", default="100,200,300,400,500,600,700,800,900,1000,1200,1500,2000",
        help="逗号分隔的前缀探针档位(ms)",
    )
    args = parser.parse_args()

    manifest = json.load(open(args.cases, encoding="utf-8"))
    hints = manifest.get("hints", [])
    cases = manifest["cases"]

    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    from funasr import AutoModel

    t0 = time.perf_counter()
    streaming = AutoModel(model="paraformer-zh-streaming", disable_update=True)
    offline = AutoModel(model="paraformer-zh", disable_update=True)
    load_s = time.perf_counter() - t0
    print(f"[funasr] models loaded in {load_s:.1f}s", flush=True)

    # 预热(不计入指标):让 onnx/torch 内核与缓存到位。
    warm = os.path.join(args.audio_dir, sorted(c["id"] for c in cases)[0] + ".wav")
    streaming.generate(input=warm, chunk_size=[0, 10, 5], encoder_chunk_look_back=4, decoder_chunk_look_back=1)
    offline.generate(input=warm)
    offline.generate(input=warm, hotword=" ".join(hints))
    print("[funasr] warmed up", flush=True)

    prefix_list = [int(x) for x in args.prefixes.split(",") if x.strip()]
    records = []
    for c in cases:
        wav_path = os.path.join(args.audio_dir, c["id"] + ".wav")
        if not os.path.exists(wav_path):
            print(f"[funasr] missing audio for {c['id']}, skip", flush=True)
            continue
        pcm = read_wav(wav_path)
        bytes_per_ms = 32  # 16k*16bit*mono
        rec = {"id": c["id"], "category": c["category"]}

        # --- partial:prefix-probe ---
        probes = []
        first_token = None
        for ms in prefix_list:
            n = min(len(pcm), ms * bytes_per_ms)
            prefix = make_wav(pcm[:n])
            t = time.perf_counter()
            res = streaming.generate(
                input=prefix,
                chunk_size=[0, 10, 5],
                encoder_chunk_look_back=4,
                decoder_chunk_look_back=1,
            )
            compute_ms = round((time.perf_counter() - t) * 1000, 1)
            text = result_text(res)
            probes.append({"prefixMs": ms, "computeMs": compute_ms, "text": text})
            if text and first_token is None:
                first_token = {
                    "audioLeadMs": ms,
                    "computeMs": compute_ms,
                    "latencyMs": ms + compute_ms,
                    "text": text,
                }
                break
        rec["partial"] = {"probes": probes, "firstToken": first_token}

        # 流式整段计算耗时(单路解码成本参考)。
        t = time.perf_counter()
        res = streaming.generate(
            input=make_wav(pcm), chunk_size=[0, 10, 5], encoder_chunk_look_back=4, decoder_chunk_look_back=1
        )
        rec["streamingFullComputeMs"] = round((time.perf_counter() - t) * 1000, 1)
        rec["streamingFullText"] = result_text(res)

        # --- final:无热词 vs 热词(全表 hints,生产同形) ---
        t = time.perf_counter()
        res_plain = offline.generate(input=wav_path)
        plain_ms = round((time.perf_counter() - t) * 1000, 1)
        t = time.perf_counter()
        res_hot = offline.generate(input=wav_path, hotword=" ".join(hints))
        hot_ms = round((time.perf_counter() - t) * 1000, 1)
        rec["final"] = {
            "plainText": result_text(res_plain),
            "plainMs": plain_ms,
            "hotText": result_text(res_hot),
            "hotMs": hot_ms,
            "hints": hints,
        }
        records.append(rec)
        print(
            f"[funasr] {c['id']} firstToken={first_token} plain={plain_ms}ms hot={hot_ms}ms",
            flush=True,
        )

    out = {
        "engine": "funasr-2pass-python",
        "models": {
            "streaming": "paraformer-zh-streaming (iic/speech_paraformer-large_asr_nat-zh-cn-16k-common-vocab8404-online)",
            "final": "seaco-paraformer (iic/speech_seaco_paraformer_large_asr_nat-zh-cn-16k-common-vocab8404-pytorch)",
        },
        "chunkSize": [0, 10, 5],
        "modelLoadS": round(load_s, 1),
        "records": records,
    }
    with open(args.out, "w", encoding="utf-8") as f:
        json.dump(out, f, ensure_ascii=False, indent=2)
    print(f"[funasr] wrote {args.out} ({len(records)} cases)", flush=True)


if __name__ == "__main__":
    main()

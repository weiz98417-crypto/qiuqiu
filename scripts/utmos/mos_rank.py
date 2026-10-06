#!/usr/bin/env python
"""UTMOSv2 批量 MOS 预测与排序（openspec/changes/eval-tooling 9.1）。

对输入目录下的 wav/mp3/flac 逐个出 1-5 分自动 MOS，按分降序输出 CSV，
供韵律盲测（tts-supply-switch）做头部粗筛——域差注记见 README：对中文
高表现力 TTS 只粗筛不下结论，人耳结论必须真实采集（ADR-0023）。

用法:
    python mos_rank.py --input path/to/samples/ --output ranking.csv
模型权重首跑自动下载（UTMOSv2 官方 checkpoint，缓存 ~/.cache/utmos）。
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import pathlib
import sys
import urllib.request

# UTMOSv2 官方 strong checkpoint（Sarulab/utmosv2 release）。
CHECKPOINT_URL = "https://huggingface.co/sarulab/utmosv2/resolve/main/utmosv2-strong.ckpt"
CACHE_DIR = pathlib.Path.home() / ".cache" / "utmos"

AUDIO_EXTENSIONS = {".wav", ".mp3", ".flac"}


def ensure_checkpoint() -> pathlib.Path:
    """下载并缓存模型权重（首跑联网，之后命中本地缓存）。"""
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    target = CACHE_DIR / "utmosv2-strong.ckpt"
    if target.exists() and target.stat().st_size > 1_000_000:
        return target
    print(f"downloading UTMOSv2 checkpoint -> {target}", file=sys.stderr)
    urllib.request.urlretrieve(CHECKPOINT_URL, target)
    digest = hashlib.sha256(target.read_bytes()).hexdigest()
    print(f"checkpoint sha256={digest[:16]}…", file=sys.stderr)
    return target


def load_model(checkpoint: pathlib.Path):
    """加载 UTMOSv2 strong 模型（import 放函数内，--help 不触发 torch）。"""
    import torch  # noqa: PLC0415 —— CLI 冷路径延迟导入
    from utmosv2 import UTMOSv2  # type: ignore[import-not-found]

    device = "cuda" if torch.cuda.is_available() else "cpu"
    model = UTMOSv2.from_pretrained(checkpoint)
    model = model.to(device).eval()
    return model, device


def predict(model, device: str, path: pathlib.Path) -> float:
    import librosa
    import numpy as np
    import torch

    wav, _ = librosa.load(str(path), sr=16000, mono=True)
    tensor = torch.from_numpy(np.asarray(wav, dtype=np.float32)).unsqueeze(0).to(device)
    with torch.no_grad():
        score = model(tensor)
    return float(score.item())


def main() -> int:
    parser = argparse.ArgumentParser(description="UTMOSv2 batch MOS ranking")
    parser.add_argument("--input", required=True, help="audio file or directory")
    parser.add_argument("--output", default="ranking.csv", help="output CSV (file,mos)")
    args = parser.parse_args()

    root = pathlib.Path(args.input)
    if root.is_file():
        files = [root]
    else:
        files = sorted(p for p in root.rglob("*") if p.suffix.lower() in AUDIO_EXTENSIONS)
    if not files:
        print(f"no audio files under {root}", file=sys.stderr)
        return 1

    checkpoint = ensure_checkpoint()
    model, device = load_model(checkpoint)
    print(f"model ready on {device}; scoring {len(files)} files", file=sys.stderr)

    rows = []
    for path in files:
        try:
            rows.append((path.name, predict(model, device, path)))
        except Exception as error:  # noqa: BLE001 —— 单文件坏档不拖垮整批
            print(f"skip {path.name}: {error}", file=sys.stderr)
    rows.sort(key=lambda row: row[1], reverse=True)

    with open(args.output, "w", newline="", encoding="utf-8") as handle:
        writer = csv.writer(handle)
        writer.writerow(["file", "mos"])
        writer.writerows(rows)
    print(f"wrote {len(rows)} scores -> {args.output}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

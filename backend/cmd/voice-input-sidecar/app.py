"""用户语音情绪 sidecar（openspec/changes/user-voice-affect 波1）。

POST /affect 收整段话轮 PCM（pcm_s16le / 16kHz / 单声道裸字节），用
sherpa-onnx 的 SenseVoice 离线模型出情绪标签，返回 {label, confidence}；
GET /healthz 供 compose healthcheck。

宪法线（与 ambient 同规格）：本服务输出的情绪标签只作球球的观测/偏置
信号——永不进比赛事实账本；音频不落盘、会话外不接收。

诚实注记：SenseVoice 输出的是情绪 token（<|HAPPY|> 等），没有校准概率，
confidence 恒为 VOICE_PSEUDO_CONFIDENCE（默认 0.6）——置信门在 Go relay
层，语义是「sidecar 给了标签」而非「模型有多确信」。真机校准留波2。

模型：sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17（int8），由
scripts/voice-model/download.mjs 从 hf-mirror 拉取（sha256 锁定），volume
挂到 VOICE_MODEL_DIR。模型未就位时 /affect 返回 503——Go 侧按失败静默
丢弃，旁路整体降级为不可用。
"""

import os
import re

import numpy as np
import sherpa_onnx
from fastapi import FastAPI, Request, Response
from fastapi.responses import JSONResponse

MODEL_DIR = os.environ.get("VOICE_MODEL_DIR", "/models/voice")
PORT = int(os.environ.get("VOICE_SIDECAR_PORT", "8092"))
PSEUDO_CONFIDENCE = float(os.environ.get("VOICE_PSEUDO_CONFIDENCE", "0.6"))
SAMPLE_RATE = 16000

# SenseVoice 情绪 token → 小写标签（Go 侧与运营台消费的词表）。
EMOTION_TOKENS = {
    "HAPPY": "happy",
    "SAD": "sad",
    "ANGRY": "angry",
    "SURPRISED": "surprised",
    "NEUTRAL": "neutral",
}
EMOTION_RE = re.compile(r"<\|(" + "|".join(EMOTION_TOKENS) + r")\|>")

app = FastAPI()
recognizer = None


def load_recognizer():
    global recognizer
    tokens = os.path.join(MODEL_DIR, "tokens.txt")
    model = os.path.join(MODEL_DIR, "model.int8.onnx")
    if not (os.path.exists(tokens) and os.path.exists(model)):
        return False
    recognizer = sherpa_onnx.OfflineRecognizer.from_sense_voice(
        model=model,
        tokens=tokens,
        use_itn=True,
    )
    return True


@app.get("/healthz")
def healthz():
    return {"ok": recognizer is not None, "modelDir": MODEL_DIR}


@app.post("/affect")
async def affect(request: Request):
    if recognizer is None:
        return JSONResponse(status_code=503, content={"error": "model not loaded"})
    pcm_bytes = await request.body()
    if len(pcm_bytes) < 320:  # <20ms 视为无信号
        return JSONResponse(status_code=400, content={"error": "pcm too short"})
    samples = np.frombuffer(pcm_bytes, dtype=np.int16).astype(np.float32) / 32768.0
    stream = recognizer.create_stream()
    stream.accept_waveform(SAMPLE_RATE, samples)
    recognizer.decode_stream(stream)
    match = EMOTION_RE.search(stream.result.text)
    if match is None:
        # 无情绪 token：按 neutral 诚实返回（置信门会照常过滤）。
        return {"label": "neutral", "confidence": PSEUDO_CONFIDENCE}
    return {"label": EMOTION_TOKENS[match.group(1)], "confidence": PSEUDO_CONFIDENCE}


if __name__ == "__main__":
    import uvicorn

    if not load_recognizer():
        print(f"[voice-input-sidecar] model not found under {MODEL_DIR}; /affect will 503")
    uvicorn.run(app, host="0.0.0.0", port=PORT)

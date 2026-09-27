# 轮次检测 sidecar（voice-turn-detection 决策 c）：LiveKit EOU 多语版
# （livekit/turn-detector v0.4.1-intl）的本地 ONNX 推理服务。
#
# 推理逻辑逐行对齐 LiveKit agents 的
# livekit-plugins-turn-detector/base.py（_EUORunnerBase.run）：
#   文本归一 → 合并相邻同角色 → apply_chat_template → 去尾部 EOU 标记 →
#   tokenizer 左截断编码（max_length=128）→ onnx 前向 → 取末位概率。
# 服务契约（watchconnection turn_query 的转发目标）：
#   POST /turn    {text, chatCtx?} → {probability, isComplete}
#   GET  /healthz → {status, revision, languages}
# sidecar 无状态、无会话概念；阈值 env 可调（默认 0.5）。模型目录
# env TURN_MODEL_DIR 指向 scripts/turn-model/download.mjs 的落盘目录。

from __future__ import annotations

import json
import logging
import math
import os
import re
import threading
import time
import unicodedata
from pathlib import Path
from typing import Any

import onnxruntime as ort
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
from transformers import AutoTokenizer

# 与 LiveKit base.py 一致的历史长度约束：128 token / 6 轮。
MAX_HISTORY_TOKENS = 128
MAX_HISTORY_TURNS = 6

MODEL_DIR = Path(os.environ.get("TURN_MODEL_DIR", "E:/tools/turn-model/v0.4.1-intl"))
PORT = int(os.environ.get("TURN_SIDECAR_PORT", "8091"))

# 判完阈值：默认取 LiveKit 官方对目标语言的校准值（languages.json，zh≈0.0067，
# 由内部评测按 TPR/TNR 折算——这个模型家族的概率量程是 0~1 全幅，凭直觉取
# 0.5 会让一切话轮都被判「没说完」）；TURN_COMPLETE_THRESHOLD 显式覆盖。
TURN_LANGUAGE = os.environ.get("TURN_LANGUAGE", "zh")
_calibrated = None
_languages_meta: dict[str, Any] = {}
_languages_path = MODEL_DIR / "languages.json"
if _languages_path.exists():
    _languages_meta = json.loads(_languages_path.read_text(encoding="utf-8"))
    _calibrated = _languages_meta.get(TURN_LANGUAGE, {}).get("threshold")
THRESHOLD = float(os.environ.get("TURN_COMPLETE_THRESHOLD") or _calibrated or 0.5)

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger("turn-sidecar")

app = FastAPI(title="qiuqiu turn-sidecar")

# 推理串行锁：q8 模型单次前向是 CPU 密集段，并发前向只会互相拖慢，
# 排队串行让延迟可预期（上游单连接的查询节奏本来就 ≈1 次/300ms）。
_inference_lock = threading.Lock()
_session: ort.InferenceSession | None = None
_tokenizer: Any = None
_languages: dict[str, Any] = {}


class TurnRequest(BaseModel):
    text: str = ""
    # 可选对话历史（含当前话轮之前的 user/assistant 消息），与 LiveKit
    # chat_ctx 同形；服务端转发目前只带 text，字段留给多轮上下文扩展。
    chatCtx: list[dict[str, str]] = Field(default_factory=list)


class TurnResponse(BaseModel):
    probability: float
    isComplete: bool


def normalize_text(text: str) -> str:
    """对齐 base.py _normalize_text：NFKC 归一、小写、去标点（保留 '-），收空白。"""
    if not text:
        return ""
    text = unicodedata.normalize("NFKC", text.lower())
    text = "".join(
        ch for ch in text
        if not (unicodedata.category(ch).startswith("P") and ch not in ["'", "-"])
    )
    return re.sub(r"\s+", " ", text).strip()


def format_chat_ctx(chat_ctx: list[dict[str, str]]) -> str:
    """对齐 base.py _format_chat_ctx：合并相邻同角色（训练分布），套模板后
    去掉模板给当前话轮补的 EOU 标记（模型要预测的正是这个位置）。"""
    merged: list[dict[str, str]] = []
    last: dict[str, str] | None = None
    for msg in chat_ctx:
        if not msg.get("content"):
            continue
        content = normalize_text(msg["content"])
        if last and last["role"] == msg["role"]:
            last["content"] += f" {content}"
        else:
            item = {"role": msg["role"], "content": content}
            merged.append(item)
            last = item
    convo_text = _tokenizer.apply_chat_template(
        merged, add_generation_prompt=False, add_special_tokens=False, tokenize=False
    )
    marker = "<|im_end|>"
    index = convo_text.rfind(marker)
    return convo_text[:index] if index >= 0 else convo_text


def load_model() -> None:
    """启动时装载 tokenizer 与 ONNX 会话；线程数对齐 LiveKit（核数一半、上限 4）。"""
    global _session, _tokenizer, _languages
    started = time.perf_counter()
    sess_options = ort.SessionOptions()
    sess_options.intra_op_num_threads = max(1, min(math.ceil(os.cpu_count() or 1) // 2, 4))
    sess_options.inter_op_num_threads = 1
    sess_options.add_session_config_entry("session.dynamic_block_base", "4")
    _session = ort.InferenceSession(
        str(MODEL_DIR / "onnx" / "model_q8.onnx"),
        providers=["CPUExecutionProvider"],
        sess_options=sess_options,
    )
    _tokenizer = AutoTokenizer.from_pretrained(
        str(MODEL_DIR), truncation_side="left", local_files_only=True
    )
    languages_path = MODEL_DIR / "languages.json"
    if languages_path.exists():
        _languages = json.loads(languages_path.read_text(encoding="utf-8"))
    logger.info(
        "model loaded from %s in %.1fs (language=%s threshold=%.4f)",
        MODEL_DIR, time.perf_counter() - started, TURN_LANGUAGE, THRESHOLD,
    )


@app.on_event("startup")
def on_startup() -> None:
    load_model()


@app.get("/healthz")
def healthz() -> dict[str, Any]:
    if _session is None:
        raise HTTPException(status_code=503, detail="model not loaded")
    return {
        "status": "ok",
        "revision": MODEL_DIR.name,
        "threshold": THRESHOLD,
        "languages": sorted(_languages.keys()),
    }


@app.post("/turn", response_model=TurnResponse)
def predict_turn(request: TurnRequest) -> TurnResponse:
    text = request.text.strip()
    if not text:
        raise HTTPException(status_code=400, detail="text is required")
    # 归一化后再查空：去标点后为空的文本（「！！！」）进模板会生成空 user
    # 话轮，EOU 被 rfind 剥掉后等于对分布外输入判定——与空文本同罪回 400。
    if not normalize_text(text):
        raise HTTPException(status_code=400, detail="text is empty after normalization")
    if _session is None or _tokenizer is None:
        raise HTTPException(status_code=503, detail="model not loaded")

    chat_ctx = [*request.chatCtx[-MAX_HISTORY_TURNS:], {"role": "user", "content": text}]
    started = time.perf_counter()
    input_text = format_chat_ctx(chat_ctx)
    inputs = _tokenizer(
        input_text,
        add_special_tokens=False,
        return_tensors="np",
        max_length=MAX_HISTORY_TOKENS,
        truncation=True,
    )
    with _inference_lock:
        outputs = _session.run(None, {"input_ids": inputs["input_ids"].astype("int64")})
    probability = float(outputs[0].flatten()[-1])
    elapsed_ms = (time.perf_counter() - started) * 1000
    logger.info(
        "turn predict: probability=%.4f complete=%s elapsed_ms=%d chars=%d",
        probability, probability >= THRESHOLD, elapsed_ms, len(text),
    )
    return TurnResponse(probability=probability, isComplete=probability >= THRESHOLD)


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=PORT)

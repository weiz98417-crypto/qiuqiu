"""轮次检测 sidecar 红测（bug 猎手）：归一化后为空的 text 绕过空文本守卫。

/turn 对 strip() 后为空的 text 有 400 守卫（"text is required"），但守卫
判的是原文；「！！！」「……」这类标点话轮 strip 后非空、_normalize_text
后为空，绕过守卫进入推理。实测（桩替身复现，2026-09-27）：

  format_chat_ctx 的空 content 检查看的是归一化**前**的原文，归一化后
  为空的当前话轮以 content="" 进入模板，最终输入是
  '<|im_start|>user\n'（空 user 话轮、EOU 标记被 rfind 剥掉）——
  模型被判了一个空话轮，服务却回 200 {probability,isComplete}，把这份
  分布外输入的结论当作用户话轮的判定（经 relay 会直接驱动客户端话轮
  收口/否决，判定失败还会喂共享熔断器）。

同一「没有可判文本」条件行为分叉："   " → 400；"。。。" → 200 空话轮
结论。正确行为：归一化后为空与空文本同罪，400 拒绝判定。

模型不在场：tokenizer/session 用忠实桩替身（注入模块全局，不走
startup 装载）。tokenizer 逐字符编码、左截断；session 输出序列长与
输入一致（BERT 家族 logits 序列长=输入序列长）。
"""

from __future__ import annotations

import sys
import types

import numpy as np
import pytest
from fastapi.testclient import TestClient

# onnxruntime/transformers 只在 load_model（startup）用到；测试进程无模型，
# 用空壳模块顶替，让 app 模块本身可导入。
_ort_stub = types.ModuleType("onnxruntime")
_ort_stub.InferenceSession = object
_ort_stub.SessionOptions = object
sys.modules.setdefault("onnxruntime", _ort_stub)
_transformers_stub = types.ModuleType("transformers")
_transformers_stub.AutoTokenizer = object
sys.modules.setdefault("transformers", _transformers_stub)

import app as turn_app  # noqa: E402


class _FakeTokenizer:
    """忠实替身：逐字符编码，apply_chat_template 用 ChatML 同形模板。"""

    def __init__(self) -> None:
        self.last_text: str | None = None

    def apply_chat_template(self, messages, **_kwargs) -> str:
        return "".join(
            f"<|im_start|>{m['role']}\n{m['content']}<|im_end|>\n" for m in messages
        )

    def __call__(self, text, add_special_tokens=False, return_tensors="np",
                 max_length=128, truncation=False):
        self.last_text = text
        ids = np.array([[ord(ch) for ch in text]], dtype=np.int64)
        if truncation and ids.shape[1] > max_length:
            ids = ids[:, -max_length:]  # 左截断，与装载配置同形
        return {"input_ids": ids}


class _FakeSession:
    """输出序列长跟随输入（真实 BERT 家族即如此）。"""

    def run(self, _names, feed):
        ids = feed["input_ids"]
        return [np.zeros((ids.shape[0], ids.shape[1], 2), dtype=np.float32)]


@pytest.fixture()
def http(monkeypatch):
    monkeypatch.setattr(turn_app, "_tokenizer", _FakeTokenizer())
    monkeypatch.setattr(turn_app, "_session", _FakeSession())
    # 不用上下文管理器：不触发 startup（模型不在场），直接用注入的桩。
    return TestClient(turn_app.app, raise_server_exceptions=False)


def test_punct_only_text_with_history_is_rejected_not_judged(http):
    # 「！！！」归一化后为空：不得以空 user 话轮的结论冒充本话轮判定。
    # 当前行为：200 {"probability":0.0,"isComplete":false}（输入是
    # '<|im_start|>user\n这球出了底线<|im_end|>\n...<|im_start|>user\n'，
    # 空话轮 OOD 输入）→ 红。
    resp = http.post(
        "/turn",
        json={
            "text": "！！！",
            "chatCtx": [
                {"role": "user", "content": "这球出了底线"},
                {"role": "assistant", "content": "对，球门球"},
            ],
        },
    )
    assert resp.status_code == 400, (
        f"normalize-empty text must be rejected like empty text, "
        f"got {resp.status_code} {resp.text!r}"
    )


def test_punct_only_text_without_history_is_rejected_not_judged(http):
    # 无历史时同样不得对空话轮出结论。当前行为：200（输入
    # '<|im_start|>user\n'）→ 红。
    resp = http.post("/turn", json={"text": "……"})
    assert resp.status_code == 400, (
        f"normalize-empty text must be rejected like empty text, "
        f"got {resp.status_code} {resp.text!r}"
    )


def test_empty_text_guard_still_holds(http):
    # 既有守卫的基线：纯空文本仍 400。
    resp = http.post("/turn", json={"text": "   "})
    assert resp.status_code == 400

# fix-mic — 按住说话按钮第二次失效

## 问题

pushToTalk 模式下，第一次按住-松手正常工作，第二次按住无响应。
根因: `_finishSentence()` 调用 `_stopRecording()` (取消 stream + stop recorder)，但未将 `_isListening` 重置为 false。第二次调用 `startListening()` 时检查 `if (_isListening) return` 直接跳过。

## 修复

`_finishSentence()` 中 pushToTalk 分支添加 `_isListening = false`。
(此修复已在上一轮代码中实施，本 change 仅验证)

## 验证

- 按住 mic 说出话 → 松手 → 再次按住 → 应正常收音
- 自由对话模式下 sentenceEnd 后自动继续 → 不受影响

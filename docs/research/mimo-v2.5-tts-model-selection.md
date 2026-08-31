# MiMo V2.5 TTS 选型与球球语音建议

更新日期：2026-08-31

## 结论

- 想直接换一个中文女声：使用 `mimo-v2.5-tts`，从 `冰糖`、`茉莉`中试听选择。两者都是官方中文女性预置音色。
- 想先用文字创造专属声音：使用 `mimo-v2.5-tts-voicedesign` 生成声音原型。
- 想让专属声音长期保持一致：保存满意的原型音频，再用 `mimo-v2.5-tts-voiceclone` 作为固定参考音频合成。
- 实时陪看优先使用 `mimo-v2.5-tts`：官方已上线其低延迟流式输出；VoiceDesign 和 VoiceClone 当前尚无真正的低延迟流式输出。

## 三款模型

| 模型 | 用途 | 音色输入 | 主要限制 |
| --- | --- | --- | --- |
| `mimo-v2.5-tts` | 使用预置精品音色合成 | `冰糖`、`茉莉`、`苏打`、`白桦`、`Mia`、`Chloe`、`Milo`、`Dean` | 不能设计或克隆音色 |
| `mimo-v2.5-tts-voicedesign` | 根据文字描述从零生成音色 | `user` 消息中的音色描述 | 不使用预置音色或参考音频；低延迟流式尚未上线 |
| `mimo-v2.5-tts-voiceclone` | 根据参考音频复刻声音 | `audio.voice` 中的 MP3/WAV Data URL | 参考音频 Base64 不超过 10 MB；低延迟流式尚未上线 |

## 官方控制方式

API 没有独立的数值型 `speed`、`pitch`、`rate`、BPM 或半音参数。语速、语调、音高走势、情绪、气息、停顿和重音通过两种方式控制：

1. `messages[].role=user`：放自然语言表演指令。
2. `messages[].role=assistant`：放真正朗读的文本，可加入 `(甜美 活泼)`、`[停顿]`、`[轻笑]`、`[语速加快]` 等标签。

球球的基础风格指令建议：

```text
20 岁左右的中文女声，声线甜美清亮但不过度夹，像熟悉的朋友陪着看球。
说普通话，口语自然，不要播音腔，不要逐字朗读。语速正常略快，句内有自然快慢变化，
陈述句句尾自然回落，不要每句话都上扬。保留轻微呼吸和真实停顿；高兴时稍微提亮，
激动时有爆发但不要尖叫或破音。
```

基础 TTS 请求应同时传入风格指令和朗读文本：

```json
{
  "model": "mimo-v2.5-tts",
  "messages": [
    {
      "role": "user",
      "content": "像熟悉的年轻女足球搭档聊天。甜美清亮但不夹，语速自然略快，句尾自然回落，有真实呼吸和情绪，不要播音腔。"
    },
    {
      "role": "assistant",
      "content": "(兴奋 活泼)球进了！[吸气]这脚传中真的太漂亮了！"
    }
  ],
  "audio": {
    "format": "wav",
    "voice": "冰糖"
  }
}
```

## 本次项目调整

- 默认音色已从英文女性音色 `Chloe` 改为中文女性音色 `冰糖`。
- TTS 请求会先发送 `user` 风格指令，再以 `assistant` 消息发送实际朗读文本。
- 后端会把 `voiceStyle`、`voiceEnergy`、`voiceSpeed` 映射为 MiMo 的中文自然语言表演指导。
- 客户端已移除 Web `playbackRate` 和 Native 二次变速，始终按模型生成的原始速度播放。

## 官方来源

- [MiMo-V2.5-TTS Series 发布页](https://mimo.xiaomi.com/mimo-v2-5-tts/index-zh)
- [MiMo-V2.5-TTS 官方使用指南](https://mimo.mi.com/docs/zh-CN/quick-start/usage-guide/audio/speech-synthesis-v2.5)
- [使用指南 Markdown](https://mimo.mi.com/static/docs/quick-start/usage-guide/audio/speech-synthesis-v2.5.md)
- [语音合成 API 字段定义](https://mimo.mi.com/static/docs/api/audio/tts.md)
- [XiaomiMiMo/MiMo-Skills](https://github.com/XiaomiMiMo/MiMo-Skills/tree/main/skills/mimo-v2-5-tts)

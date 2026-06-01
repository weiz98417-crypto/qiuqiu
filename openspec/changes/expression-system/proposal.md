# Expression System — 表情与动作触发

## 目标

让球球有丰富自然的情绪表达。根据比赛事件、用户互动和对话上下文自动切换表情和动作，通过 Live2D 参数平滑过渡。

## 范围

| 模块 | 内容 |
|------|------|
| 7 种情绪状态 | idle/listening/speaking + excited/nervous/regret/tease/surprised/happy/confused |
| 事件→表情映射 | 19 种比赛事件类型 → 对应表情+强度+持续时长 |
| 表情过渡 | Blend 平滑切换 100-500ms，无硬切 |
| 表情防抖 | 1s 内不切换，同分钟内 <3 次 |
| 基础动作 | 呼吸、眨眼、歪头、点头、挥手 |
| TTS联动 | 情绪参数驱动 TTS 语速/音调 |

## 不在范围

- 物理模拟（头发/衣服摆动）
- 名人语音彩蛋表情

## 依赖

- `live2d-character` — Live2D 渲染基础
- `event-engine` — 比赛事件输入
- `voice-interaction` — 用户交互输入

## 里程碑

- [ ] 进球后 < 200ms 切换 excited
- [ ] 过渡无突变无闪烁
- [ ] 同分钟切换 < 3 次
- [ ] idle 有自然呼吸+眨眼

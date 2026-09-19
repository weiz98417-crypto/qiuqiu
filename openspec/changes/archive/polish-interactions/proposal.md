# polish-interactions — 交互打磨: 话痨 + 音频 + 动作

## 范围

三项小改动，都是提升交互体验:

### 1. 话痨调节接通

**现状:** Flutter 设置页可选安静/标准/活跃，但后端 cooldown 未消费。
**修复:** `user_speech` 消息中带 `talkativeness`，后端根据值调 `Cooldown.SetBase()`: 安静→15s, 标准→8s, 活跃→4s。

### 2. SoLoud 音频播放间隙

**现状:** 50ms drain timer 每帧独立 `loadMem`+`play`，PCM 碎片化产生 audible gap。
**修复:** 积累 200ms PCM(4帧) 后打包一个 WAV 播放。

### 3. 动作(motion)驱动

**现状:** 模型有 9 个 motion 完全没用。
**修复:**
- 比赛事件: 开始→hello, 进球→idle_02(欢呼)
- 用户互动: 说话后→listen, 回复后→speak

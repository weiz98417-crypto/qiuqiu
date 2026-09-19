# Talkativeness Control 技术设计

## 1. 三档参数

| 参数 | 安静 | 标准 | 活跃 |
|------|------|------|------|
| 触发优先级 | P0 only | P0+P1+P2 | P0+P1+P2+P3 |
| 冷却时间 | 30s | 8s | 4s |
| 间隔倍率 | ×2.0 | ×1.0 | ×0.5 |
| 主动说话 | 否 | 偶尔(10min) | 是(3min) |
| 5分钟最大事件数 | 3 | 8 | 15 |

## 2. 实现方案

### 2.1 配置结构

```go
type TalkativenessConfig struct {
    AllowedPriorities    []int         `json:"allowed_priorities"`
    CooldownSec          int           `json:"cooldown_s"`
    IntervalMultiplier   float64       `json:"interval_multiplier"`
    AutoSpeakEnabled     bool          `json:"auto_speak_enabled"`
    AutoSpeakIntervalSec int           `json:"auto_speak_interval_s"`
    MaxEventsPer5Min     int           `json:"max_events_per_5min"`
}

var levels = map[string]TalkativenessConfig{
    "quiet":  {AllowedPriorities: []int{0}, CooldownSec: 30, IntervalMultiplier: 2.0, AutoSpeakEnabled: false, MaxEventsPer5Min: 3},
    "normal": {AllowedPriorities: []int{0,1,2}, CooldownSec: 8, IntervalMultiplier: 1.0, AutoSpeakEnabled: true, AutoSpeakIntervalSec: 600, MaxEventsPer5Min: 8},
    "active": {AllowedPriorities: []int{0,1,2,3}, CooldownSec: 4, IntervalMultiplier: 0.5, AutoSpeakEnabled: true, AutoSpeakIntervalSec: 180, MaxEventsPer5Min: 15},
}
```

### 2.2 集成到 Engine

```go
func (e *Engine) SetTalkativeness(level string) {
    cfg := levels[level]
    e.cooldown.SetBase(cfg.CooldownSec)
    e.throttler.SetMultiplier(cfg.IntervalMultiplier)
    e.allowedPriorities = cfg.AllowedPriorities
}
```

### 2.3 切换流程

```
用户点档位 → 立即生效 → 球球语音确认:
  安静: "好～我少说两句，专心看球～"
  标准: "行，我正常陪你看～"
  活跃: "好嘞！今天多说点！"
```

## 3. 主动说话（活跃模式）

条件全部满足时触发：
- 活跃模式
- > 3 分钟无事件
- > 3 分钟无用户互动
- 比赛中

内容从预置池随机抽取：
- "好久没射门了...两边都在试探啊"
- "比赛有点沉闷啊，你觉得呢？"
- "马上{minute}分钟了，看看会不会有什么变化"

## 4. UI（Flutter）

```
┌──────────────────────────────────┐
│   安静        标准        活跃    │
│    ○          ◉          ○      │
│   小声        刚好        话多    │
└──────────────────────────────────┘
```

`SegmentedButton` 三段式，切换时通知服务端更新档位。

## 5. 偏好持久化

Flutter 端 SharedPreferences 存储 `talkativeness` 值。
WebSocket 连接时发送给服务端。

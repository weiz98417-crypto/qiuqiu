# Expression System 技术设计

## 1. 表情状态全集

| 状态 | 触发 | 优先级 | 持续 |
|------|------|--------|------|
| idle | 默认 | 最低 | 永久 |
| listening | 用户说话 | 6 | 说话期间 |
| speaking | 球球说话 | 最高(叠加) | 说话期间 |
| excited | 进球/点球 | 8 | 5-8s |
| nervous | 射门/紧张 | 7 | 3s |
| regret | 失球/失误 | 6 | 5s |
| tease | 黄牌/越位/吐槽 | 5 | 2-3s |
| surprised | 红牌/神扑 | 9 | 5s |
| happy | 比赛开始/称赞 | 4 | 3s |
| confused | VAR/疑惑 | 3 | 3s |

## 2. 事件→表情映射表

```go
var eventExpression = map[string]ExpressionConfig{
    "goal":          {State: "excited", Intensity: 1.0, Duration: 5 * time.Second},
    "goal_winner":   {State: "excited", Intensity: 1.0, Duration: 8 * time.Second},
    "own_goal":      {State: "surprised", Intensity: 0.9, Duration: 5 * time.Second},
    "red_card":      {State: "surprised", Intensity: 0.9, Duration: 5 * time.Second},
    "yellow_card":   {State: "tease", Intensity: 0.4, Duration: 3 * time.Second},
    "penalty":       {State: "nervous", Intensity: 0.8, Duration: 5 * time.Second},
    "penalty_missed":{State: "surprised", Intensity: 1.0, Duration: 5 * time.Second},
    "shot":          {State: "nervous", Intensity: 0.6, Duration: 3 * time.Second},
    "match_start":   {State: "happy", Intensity: 0.6, Duration: 3 * time.Second},
    "match_end":     {State: "normal", Intensity: 0.5, Duration: 5 * time.Second},
    "var_check":     {State: "confused", Intensity: 0.5, Duration: 3 * time.Second},
    "offside":       {State: "tease", Intensity: 0.3, Duration: 2 * time.Second},
}
```

## 3. 防抖机制

```go
func (e *ExpressionEngine) Set(expr ExpressionConfig) {
    if time.Since(e.lastChange) < 1*time.Second {
        if expr.Priority <= e.current.Priority {
            return // 更弱或同级 → 忽略
        }
    }
    if e.changesThisMinute >= 3 {
        return // 同分钟超限
    }
    e.transition(expr)
}
```

## 4. 表情过渡

```
过渡矩阵（ms）:
         → excited nervous regret tease happy
idle       300     250     250   200   200
excited     -      200     300   300   200
nervous    200      -      200   200   200
regret     300     200      -    200   300
tease      300     200     200     -   200
happy      200     200     300   200    -
```

Live2D 侧做 ease-out 插值。表情强度随时间自然衰减（每秒 30%）。

## 5. 基础动作

| 动作 | 实现 | 频率 |
|------|------|------|
| 呼吸 | ParamBreath sin 周期 | 持续 |
| 眨眼 | ParamEyeOpen 0→1 脉冲 | 随机 2-5s |
| 歪头 | ParamAngleZ 0→-15 | 疑惑时 1 次 |
| 点头 | ParamAngleX 0→-10→0 | 用户说话时，10s 最多 1 次 |

## 6. TTS 情绪联动

| 表情 | TTS 语速 | TTS 音调 |
|------|---------|---------|
| excited | +15% | +10% |
| nervous | +5% | -5% |
| regret | -10% | -10% |
| tease | +10% | +5% |
| happy | +5% | +5% |
| normal | 默认 | 默认 |

## 7. 与 Flutter 通信

服务端发送 expression 指令 → Flutter 调用 Live2D JS Bridge：

```json
{ "type": "expression", "state": "excited", "intensity": 1.0 }
```

Flutter 侧 `Live2dWidget.expression` 属性更新 → `evaluateJavascript("setExpression('excited')")`

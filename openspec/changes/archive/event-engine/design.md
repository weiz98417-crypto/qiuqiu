# Event Engine 技术设计

## 1. 事件管道

```
api-sports.io (每3s poll)
    │
    ▼
标准化事件 ← match状态跟踪（差分检测新事件）
    │
    ▼
[去重] Redis Key: dedup:{match_id}:{type}:{team}:{minute}, TTL 30s
    │
    ▼
[节流] Redis Key: throttle:{match_id}:{event_group}, TTL = 最小间隔
    │  配置：
    │    goal: 0s    yellow_card: 0s
    │    shot: 15s   foul: 30s
    │    corner: 60s offside: 60s
    │
    ▼
[优先级队列] P0 → 立即, P1-P3 → 排队, P4 → 丢弃
    │
    ▼
[冷却检查] 动态冷却：基础间隔 8s，密集时段自动缩短至 5s，平静时段延至 12s，P0 可跳过
    │
    ▼
[上下文增强] 比分变化/帽子戏法/绝杀/扳平/时间语境
    │
    ▼
[输出] AIGenerationInstruction → Go channel (buffer=1024, 满时丢弃 P4 事件) → ai-pipeline
```

## 2. 核心数据结构

```go
// MatchState 比赛状态快照（内存）
type MatchState struct {
    MatchID        string
    HomeTeam       string
    AwayTeam       string
    HomeScore      int
    AwayScore      int
    Minute         int
    Half           int
    Status         string // 'live'|'ht'|'ft'|'postponed'
    RecentEvents   []Event // 最近 10 个
    LastSpeakTime  time.Time
    CooldownUntil  time.Time
    PlayerGoals    map[string]int // player_name → count
}

// AIGenerationInstruction 发给 ai-pipeline 的指令
type AIGenerationInstruction struct {
    InstructionType string       // "event_reaction"
    Event           StandardEvent
    MatchContext    EnrichedContext
    Expression      string       // "excited"|"nervous"|"normal"|"tease"|"regret"
    Priority        int
}
```

## 3. 上下文增强逻辑

```go
func EnrichContext(event Event, state MatchState) EnrichedContext {
    return EnrichedContext{
        ScoreBefore:   state.PreviousScore,
        ScoreAfter:    state.CurrentScore,
        IsEqualizer:   state.HomeScore == state.AwayScore,
        IsWinner:       event.Minute > 85 && isLeadingAfter,
        IsHatTrick:     state.PlayerGoals[event.Player] >= 2, // 加上这次=3
        TimeContext:    getTimeContext(event.Minute),          // "开局"/"中段"/"尾声"
        HalfContext:    getHalfContext(event.Half),
        Significance:   calcSignificance(event, state),       // "首开纪录"/"反超球"/"绝杀"
    }
}
```

## 4. 数据源轮询

```go
// 每 3s 轮询一次
ticker := time.NewTicker(3 * time.Second)
for range ticker.C {
    events, err := apiSports.FetchEvents(matchID, lastEventID)
    // 差分检测：只处理新事件
    for _, e := range events {
        if e.ID > lastEventID {
            pipeline <- e
            lastEventID = e.ID
        }
    }
}
```

## 5. 事件定义

```go
type StandardEvent struct {
    ID        int64  // api-sports 的递增数字 ID，用于差分检测
    Type      string // goal|shot|yellow_card|red_card|penalty|foul|corner|offside|substitution|var_check|match_start|match_end
    Team      string // "home"|"away"
    Minute    int
    Player    PlayerInfo
    Score     ScoreInfo
    Timestamp time.Time
}

// 差分检测：lastEventID 为 int64
// if e.ID > lastEventID { ... }
```

# Design: Client Audio & WS Resilience

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | AudioSource 生命周期：单字段跟踪「当前来源」，新播放/pause/dispose 三处释放；释放逻辑抽 AudioSourceLifecycle 纯类（持引用、dispose、幂等），播放器持有它——SoLoud 本体不可注入测试，生命周期类可测。 |
| 2 | WS：_open 的 close 移入既有 try（吞错）；send 返回 false 时调用 _scheduleReconnect（幂等：已有重连计时则忽略）。 |
| 3 | 不改后端与协议。 |

## Seam

- AudioSourceLifecycle 类与 WebSocketService 的公开方法即测试面（websocket_service_test.dart 已有 harness 直接复用）。

## Testing decisions

- AudioSourceLifecycle：新建→释放→再新建 幂等性单测。
- WS：close 抛错不致死（stub channel 抛异常）；send 失败进入 reconnecting 状态两条单测。
- 回归：flutter test 全绿。
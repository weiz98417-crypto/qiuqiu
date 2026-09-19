# Tasks: Client Audio & WS Resilience

- [x] 1.1 AudioSourceLifecycle 抽取 + 三处释放接线。
- [x] 1.2 生命周期单测（幂等释放）。
- [x] 1.3 WS close 入 try；send 失败触发重连。
- [x] 1.4 WS 两条单测（close 抛错、send 失败重连）。
- [x] 1.5 验证：flutter test 全绿 + dart analyze 零 warning。

## Sequencing

打磨轮第二个。
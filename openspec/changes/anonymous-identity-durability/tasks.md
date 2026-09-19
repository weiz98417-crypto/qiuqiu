# Tasks: Anonymous Identity Durability

- [x] 1.1 deviceId 存取收敛函数对（secure 优先 + prefs 回退 + 双写 + 首次迁移）。
- [x] 1.2 核实 401 重建路径复用同一 deviceId；偏差即修。
- [x] 1.3 后端 409 ErrIdentityUnavailable 时客户端如实重置。
- [x] 1.4 表驱动单测：迁移/双写/回退/重建复用/409 重置。
- [x] 1.5 CONTEXT.md 已增「匿名身份」词条（随本变更提交）。
- [x] 1.6 验证：flutter test 全绿 + dart analyze 零 warning。

## Sequencing

打磨轮第一个：数据丢失级，正确性修复。
# Design: Anonymous Identity Durability

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | deviceId 读写收敛到 preferences_service 的单一函数对：读 = secure 优先 → SharedPreferences 回退；写 = 双写（secure 为主、prefs 保留一份兜底），杜绝换存储生成新 UUID。 |
| 2 | 401 重建路径：session_service 清令牌后重建会话必须继续读同一 deviceId 源；核实其当前实现并在测试中锁定「重建 → 后端归还同一 usr_」。 |
| 3 | 后端 409（隐私删除/映射过期）：客户端清除本地 deviceId 与画像缓存，视为新用户开始——不重试、不降级。 |
| 4 | 不引入后端改动；Android 卸载丢失为已知边界（ADR-0001）。 |

## Seam

- deviceId 存取函数（preferences_service 内）即测试面：迁移语义、双写、回退全部表驱动直测，无需渲染。

## Testing decisions

- 单测：迁移（prefs 有值 + secure 空 → 迁移且不换值）、双写、secure 不可用回退、401 重建复用同一 deviceId（fake session api 断言上送的 deviceId 不变）。
- 既有 117 条 flutter 测试全绿为回归门。
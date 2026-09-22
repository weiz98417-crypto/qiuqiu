# Design: Live2D Motion Revert

## 映射原则

动作=相位（listen/speak/think/idle/hello），表情=情绪（13 词表情词表→7 个原装 exp3 文件不变）。事件/acts 段语义名保留，动作位全部落原装组：

| 语义名 | 原装落点（动作位） | 表情位不变 |
|---|---|---|
| celebrate | `speak` 组（Speak_01/02 随机变体） | excited |
| miss | `idle` 组变体 | sad |
| complain | `speak` | nervous/angry |
| tense | `think` | tense 词的表情位（nervous） |
| analysis | `think` | thinking |
| agree/nod | `listen` | chat |
| wave | `hello` | happy |

具体落点在实施时按动作内容微调（tasks 记录最终表）；原则：庆祝类→speak（有能量）、消沉类→idle、判罚紧张→think。

## 单源化

- Go 端唯一来源 `relationship/presentation_table.go`（acts 象限 + events 段），`presentation-map.json` 由词表测试与 Go 表对齐（现有 conformance 模式）。
- `backchannel.Decide` 改调 `relationship` 包查表（或注入函数，避免包环：backchannel 在 companion 侧，relationship 是叶子——用函数注入 `func(eventType string) (expression, motion string)`）。
- `var_check`：微反应=tense/tense；goal/goal_cancelled/var_overturn/shot_missed 照 events 段语义落原装。

## 迁移

删文件+改 model3.json 一并提交；客户端 build 目录的旧拷贝不手改（构建产物）。evals 中引用 celebrate/miss 等动作名的断言改为断言表情位或新落点。

# Tasks: Knowledge Players

- [x] 1.1 扒料：12 支豪门球队档案（AnySearch 检索 + extract，双源交叉）。
- [x] 1.2 扒料：8–10 名顶星球员档案。
- [x] 1.3 策展落库 `backend/knowledge/players/*.yaml`（answer 口语化、source/effective_at 齐全、快变字段带保质期措辞）。
- [x] 1.4 ADR-0017 补转会窗复查制度后果条款。
- [x] 1.5 检索验证：球员名/队名 topic 命中 + 换说法向量命中 + 非足球问句不误触，单测。
- [x] 1.6 验证：全量 go test + eval 绿。

## 触发型留尾（Q3 标准格式）

- 触发：data-provider-lite-bridge 落地；动作：源数据替换策展条目。
- 触发：转会窗关闭（每年 7 月 / 1 月）；动作：快变字段集中复查一轮。

## Sequencing

打磨轮 3 与 settings-in-policy、memory-in-policy 之后（无硬依赖，排后因工具刚就绪、且知识扩展优先级低于信任还债）。

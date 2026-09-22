# Tasks: Knowledge RAG

- [x] 2.1 internal/knowledge：条目加载/双路检索/确定性回答 + 首批 10 条策展条目。
- [x] 2.2 knowledge_question 意图（注册表 + router 枚举/prompt 增行 + handler）。
- [x] 2.3 ADR-0017 + CONTEXT.md「知识条目」词条。
- [x] 2.4 单测：双路检索合并、无命中如实回话、意图置信门。
- [x] 2.5 验证：全量 go test + eval 绿。

## Sequencing

第二波第 2 个；依赖 semantic-memory 的 embedding 通道（缺席时自动退关键词单路）。

## 触发型留尾（Q3 标准格式）

- 触发：data-provider-lite-bridge 落地；动作：权威源接入知识域（knowledge-players change 已承接球队/球员档案，规则/赛制条目复核其来源表述）。

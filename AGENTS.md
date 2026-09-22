## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

When the user types `/graphify`, use the installed graphify skill or instructions before doing anything else.

Rules:
- In this Windows checkout, invoke Graphify through the project interpreter: `& (Get-Content graphify-out/.graphify_python) -m graphify <command>`. The package is intentionally not installed on the global `PATH`.
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- Dirty graphify-out/ files are expected after hooks or incremental updates; dirty graph files are not a reason to skip graphify. Only skip graphify if the task is about stale or incorrect graph output, or the user explicitly says not to use it.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).

## 登录与账号

客户端登录相关改动先读 `docs/adr/0020-end-user-login-credential-seam.md`（凭证缝决策 + 上线资格包分期表）。当前形态：邮箱+密码镜像运营端，匿名身份仍是默认形态；短信/微信/Apple/Passkey/一键登录/未成年人模式全部后置，各自的触发条件以 ADR-0020 的资格包表为准，不要提前实现。

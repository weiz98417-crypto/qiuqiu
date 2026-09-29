# MCP 工具层:ToolRegistry 单一源与账本只读 server(波1)

## Why

工具缝是描述性镜像:CompanionToolSchemas 23 项纯审计镜像(companion/boundary.go:44-193,deletion test 不过——删除零行为变化),执行硬编码在意图 handler,加一个能力=注册表五件套+handler+router 枚举三处镜像同步;生态工具一个接不进。反向:体育数据 MCP server 生态全是玩具级(10★/无 license),qiuqiu 自持账本——把账本暴露为只读 MCP server 是免开发消费端(Claude/Cursor/运营辅助直查)。xiaozhi-esp32-server 已验证「意图注册表(静态确定性契约)+ MCP(动态供给)」分层的工程可行性。

## What Changes(波1:注册表单一源 + 反向只读 server)

- **ToolRegistry 单一源**:`interface { Name; Schema; Invoke }`;内置 handler 入池注册;trace 的 ToolCall 名称与 CompanionToolSchemas 的 schema **从注册表生成**——三处镜像消失,加能力=一处注册。工具调用 trace 字段按 `gen_ai.tool.*` 定名(trace-genai-alignment 交叉约定)。
- **反向只读 MCP server**:官方 go-sdk(modelcontextprotocol/go-sdk,锁版本)server 形态,Streamable HTTP 挂现有 server `/mcp` 路径(ServeMux 已声明式收敛,不起新进程新端口);鉴权=运营台 JWT 同体系、只读 scope,匿名禁用;工具集四个只读:`match.read_snapshot` / `match.search_events` / `match.get_player_timeline` / `schedule.read_today`——内部直调 matchstate/schedule 既有读函数,零新逻辑。
- **治理与红线(为波2 立法,随本 change 落文档+测试)**:MCP server 白名单进 config;工具时限预算(起步 2s)+失败静默降级;**比赛数据类 MCP 工具禁入,MCP 工具结果永不进比赛事实账本**(宪法负例测试,与气氛/用户情绪同规格);正向消费(波2)触发模型=意图 handler 的实现细节,router/置信门/降级链不动(ADR-0009 不修订)。
- ADR-0022 新增:MCP 工具层——注册表供给分层与事实红线。
- Flutter 侧 mcp_dart(MIT)备案不施工(未来客户端本地工具走同协议)。

## User Stories

1. As a 意图开发者, I want 加工具只注册一处, so that 不再三处镜像同步、deletion test 过关。
2. As a 运营/外部 agent 用户, I want 用 MCP 客户端直查账本, so that 核对比赛事实不用开运营台。
3. As a 守门人, I want 工具结果进不了事实账本, so that 账本唯一真相源(ADR-0002)不被外部数据污染。

## Non-goals

- 正向 MCP client 消费外部工具(波2;白名单 server 按届时真实需求选型——调研所见现成的均为玩具,不为接而接)。
- router 工具列表动态化 / tool-loop(ADR-0009 修订议题,真实需求出现再开)。
- 写类 MCP 工具(只读,永远)。
- mcp_dart 施工(备案)。
- CompanionToolSchemas 的 23 个名称语义变化(注册表生成时保持名称兼容,trace 消费方零迁移)。

## Success Criteria

- deletion test:ToolRegistry 删则三处镜像复杂度回到调用方——真 seam(两 adapter:内置 handler + 波2 MCP);
- 真实 MCP 客户端(MCP inspector 或 Claude)实测四工具查询往返;
- JWT 只读 scope:匿名拒、写调用拒、越 scope 拒;
- 宪法负例:MCP 路径对账本零写入;
- trace ToolCall 与注册表单一源一致性断言;
- go 全量 + pr tier 绿。

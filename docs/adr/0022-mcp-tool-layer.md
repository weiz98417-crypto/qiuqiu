# 0022 · MCP 工具层：注册表供给分层与事实红线

日期：2026-09-30 · 变更：openspec/changes/mcp-registry-serve · 状态：已接受

## 背景

工具缝曾是「描述性镜像」：CompanionToolSchemas 23 项纯审计镜像（deletion test 不过——删除零行为变化），执行硬编码在意图 handler，加一个能力要注册表五件套 + handler + router 枚举三处镜像同步；生态现成工具一个接不进。同时 qiuqiu 自持的比赛事实账本在体育数据 MCP 生态（全是玩具级：10★/无 license）反而是稀缺资产，值得反向输出。

## 决定

1. **静态契约与动态供给分层**（xiaozhi-esp32-server 已验证的工程形态）：ADR-0009 的意图注册表是静态确定性契约层——关键词快通道、router 枚举、置信门、降级链全部不动；MCP 是长尾动态供给层。意图 handler 多一种「调 MCP 工具」的实现，router 不感知工具列表。
2. **ToolRegistry 单一源**：`companion.ToolRegistry`（MustRegister/Schemas/Allowed，重名装配期 panic）是工具描述的唯一来源——CompanionToolSchemas 与运营只读门从注册表出；MCP 端 InputSchema 由注册表描述生成；trace 工具名常量（tracefields.go）与之同源。加一个工具 = 常量 + 一条描述。
3. **反向先行，只读永远**：`/mcp` 挂只读 MCP server（官方 go-sdk v1.8.0，Streamable HTTP），四只读工具（match.read_snapshot / match.search_events / match.get_player_timeline / schedule.read_today）直调既有读函数；鉴权 = 运营台 ADR-0010 同体系（JWT/个人令牌 → operator:trace:read），**刻意不继承 legacy APP_TOKEN 旁路——匿名一律 401**。写类 MCP 工具永不注册。
4. **事实红线（宪法级）**：比赛数据类 MCP 工具禁入，**MCP 工具结果永不进比赛事实账本**——外部数据进账本的唯一入口是运营 provisional 流程（ADR-0002）。负例测试三闸锁定（四工具 MutatesMatchFacts 全 false / 包源无写路径标识 / 未登记写语义名协议层报错）。
5. **治理（波2 生效）**：正向消费（qiuqiu 作 MCP client 调外部工具）= 意图 handler 的实现细节；server 白名单进 config（default deny）、每工具 2s 预算、失败静默降级（sidecar 纪律）。首个接入的 server 按届时真实需求选型——不为接而接。

## 被否决的替代方案

- **router 工具列表动态化 / tool-loop**（LLM 直接选工具）：需修订 ADR-0009 的单次 function call 契约，当前无真实需求；出现时单独开修订议题。
- **比赛数据 MCP 工具（标注来源的旁证比分）**：账本说 1:0、工具说 2:0 时球球说什么？用户信任陷阱，红线禁止。
- **体育数据 MCP server 消费**：玩具级生态，自持 api-sports + 账本在质量上领先。

## 后果

- 加能力从三处镜像变一处注册；deletion test 通过（注册表删则描述复杂度回调用方——真 seam）。
- 外部 agent（Claude/Cursor/MCP inspector）可直查 qiuqiu 账本，运营辅助免开发。
- Flutter 侧 mcp_dart 备案：未来客户端本地工具走同协议，本轮不引入。

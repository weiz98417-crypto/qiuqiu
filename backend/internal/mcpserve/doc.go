// Package mcpserve 把 qiuqiu 比赛事实账本暴露为只读 MCP server
// （openspec/changes/mcp-registry-serve task 5.2）：官方 go-sdk
// （github.com/modelcontextprotocol/go-sdk，锁版本）Streamable HTTP 形态，
// 挂现有 server 的 /mcp 路径，不起新进程新端口。
//
// # 工具集（只读，永远）
//
// 四个只读工具，名字与描述来自 companion.ToolRegistry 单一源
// （internal/companion/boundary.go 的注册表），本包不定义第二份描述：
//
//	match.read_snapshot        比分/时钟/节段/球队/近期事件摘要
//	match.search_events        近期 active 事件列表（limit 钳制 ≤50）
//	match.get_player_timeline  单球员事件时间线（limit 钳制 ≤50）
//	schedule.read_today        今日赛程（scope 参数仅为注册表兼容而收，
//	                           波1 只供 today 一种口径）
//
// 实现直调 matchstate 公共读（PublicSnapshot/PublicEvents）与
// companion.ScheduleReader 既有读函数，零新事实逻辑。
//
// # 鉴权（ADR-0010 运营台同体系）
//
// /mcp 走运营台凭证体系（JWT 人通道或个人令牌机通道，由 cmd/server 装配
// 时注入 Authenticator）；持 director 或 auditor 任一角色（即含只读 scope
// operator:trace:read）可读，缺 scope 403，匿名与无效凭证一律 401——
// 旧版共享 APP_TOKEN 的开发旁路对本端点不生效，MCP 永不匿名。
//
// # 红线（宪法负例测试守护，redline_test.go）
//
// 本包导出面没有任何账本写路径：工具定义全部 MutatesMatchFacts==false
// （与 companion 注册表一致），包源码不引用账本写方法；未登记工具名的
// tools/call 一律协议层报错。MCP 工具结果永不进比赛事实账本（ADR-0002：
// 账本唯一真相源；与气氛/用户情绪同规格的外部数据隔离）。
//
// # 治理（为波2 正向消费立法；波1 只文档化，不建 config、不实现消费）
//
//   - 出站白名单 config 结构语义（波2 消费用）：出站 MCP client 只允许
//     连接 config 显式列出的 server，结构按「每 server 一条：名称、
//     base URL、凭证引用（env 引用而非明文）、enabled、工具子白名单、
//     超时预算毫秒数」建模；未列出的 server 一律默认拒绝（default deny），
//     工具子白名单为空 = 该 server 一个工具都不消费。波2 施工时落
//     config 字段与本语义对齐；本包不预留任何全局变量或配置读取。
//
//   - 工具时限预算（起步 2s）：每次出站工具调用携带 2 秒墙钟预算，超时
//     视同失败。预算属于消费端契约，服务端本包不实现计时。
//
//   - 失败静默降级：工具调用失败/超时/返回错误时，消费路径静默放弃该
//     结果——不阻塞回合、不编造数据、不把失败写进任何账本；降级事件只
//     进 trace。正向消费（意图 handler 是否触发 MCP 工具）是波2 实现细节，
//     router/置信门/降级链不动（ADR-0009 不修订）。
package mcpserve

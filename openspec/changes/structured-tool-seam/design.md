# Design: Structured Tool Seam

## 形状

`structured.Extract[T](ctx, client, opts) (T, error)`：
- `jsonschema.Reflect(&zero)` 从 T 反射生成参数 schema（json tag=属性名）；
- payload 强制 `tool_choice={type:function, function:{name:ToolName}}`，thinking disabled，MaxTokens/Temperature 由 opts 声明；
- 响应只解析唯一需要的形状（第一条消息里名为 ToolName 的 function call 实参），`json.Unmarshal` 进 T；
- 错误面四类：encode/transport（openaicompat 原样上抛）/decode/无工具调用。

两个 adapter：directordraft（本波迁入）、router（待迁标记，单独立项）。

## directordraft 迁移

- `NewLLMExtractor(*structured.Client)`；main.go 用与 llm.NewClient 同一组 MIMO 凭据构造。
- prompt 唯一改动：`只输出一个 JSON 对象，不要 Markdown：{...}` 一行替换为「通过 extract_match_event 工具调用返回，字段含义以工具 schema 为准」——事件类型枚举、角色纪律、名单上下文原文保留。
- `extractJSONObject`（大括号截取）删除。

## 占位处置（删除测试命中清单）

| 占位 | 处置 | 依据 |
|---|---|---|
| llm.StreamWithMessages/StreamChunk | 删 | 全仓零消费方；SSE 语义留档于 openaicompat 注释 |
| llm.setAuthHeaders / router.setAuthHeaders | 删 | 零调用（各自走 openaicompat.Post 内部鉴权） |
| companion.AgentBoundaryResponse | 删 | 全仓零消费方（进程边界 DTO 只剩 Request 侧有用） |
| MaxTokens 魔法数 80×4 处 | 具名 defaultMaxTokens + 注释 | 意图显式化，行为不变 |

## 依赖

invopop/jsonschema v0.12.0（传递依赖 easyjson、go-ordered-map），经 goproxy.cn 引入；这是后端第 4 个直接依赖，符合「schema 反射这个能力值得一个维护中的库」的取舍。

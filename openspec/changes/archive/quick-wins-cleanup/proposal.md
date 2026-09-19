# Quick Wins Cleanup: 化石删除与浅 module 收敛

## Why

架构评审发现一批删除测试通过的化石与复制粘贴，每一项都是「下一个编辑者选错一份」的坑：零调用方的旧意图分类学 `pipeline.ClassifyIntent`（与 companion 的 12 意图体系已静默分叉）；死代码 `generateProactiveText`（连带 prompts/v1.0 的第四套人格 prompt）；llm client 内置的第三套「你是球球」人格；服务端与 agent 各一份的主动兜底罐头库；console 里渲染期 JWT 解码与 whoami 竞争的双身份源、不存在的路由跳转、跨页面复制粘贴的 trace 证据 UI、三处事件词汇表枚举、以及若干死导出与透传组件。单项都小（各 < 半天），合计一次清理。

## What Changes

**后端化石（已核实仓库级零调用方）**
- 删 `pipeline.ClassifyIntent`；`generateProactiveText` 删除后顺藤核实 PromptManager / prompts v1.0 死链，死则连删。
- llm client 移除内置默认人格 prompt；latency-test 调用方显式传 system message。
- 主动兜底罐头库合并为一处（live 调用方不变，删除重复的那份）。

**后端 seam 小合并**
- operatorauth 可选能力具名化（OperatorLister / OperatorRevoker / PasswordAccounts / RefreshTokens），删除各调用点的匿名 interface 探测与双签名 Delete 探测，501 降级文案一处。
- portrait 双面孔（用户面 / 运营代操作面）共享 view mapper 与 5-case 错误映射，各留一份。

**console 前端**
- 身份单源：OperatorProvider 唯一暴露运营员身份（登录/刷新时解码一次，机器令牌会话用 whoami 兜底），删除渲染期逐次解码。
- 修登出后跳转到不存在路由的问题。
- 「为什么说话」证据 UI（reason codes + matchesCitation + Drawer）合并为一个 TraceEvidence module，Match / CitationAudit 共用。
- 线程状态标签映射并入 format.ts（User / Threads 共用）。
- 事件词汇表单源：以 event-model 的 definition 为源，FactTimeline / MatchSettings 引用而非各写一份。
- 删透传组件 ReactRouterLink（~15 处机械替换）与死导出（SCOPE_TRACE_READ、getAccessToken/setAccessToken）。

## User Stories

1. As a agent 维护者, I want 意图分类学只有一份, so that 加新意图时不会误改早已分叉的死分类学。
2. As a agent 维护者, I want 仓库里「球球人格」prompt 只有真实在用的一份, so that 调人格时不改错文件。
3. As a 主动回合维护者, I want 兜底罐头库只有一份, so that 补一条话术不需要改两处。
4. As a console 运营员, I want 页面身份来源唯一且稳定, so that 刷新后不出现姓名闪烁或过期 scope。
5. As a console 运营员, I want 登出后落到登录页, so that 不会停在空白页猜发生了什么。
6. As a console 维护者, I want 改 trace 语义（加 reason 码来源）只动一个文件, so that Match 页与引用审计页不会漂移。
7. As a console 维护者, I want 事件类型标签只在一处定义, so that 新事件类型上线三个页面同时正确。
8. As a 后端维护者, I want operator 存储的可选能力是具名 interface, so that 加一个能力不用在 N 个调用点重写匿名断言。
9. As a 后端维护者, I want portrait 的错误映射只有一份, so that 用户面与运营面不会一个 410 一个 500。
10. As a 代码库读者, I want 死代码消失, so that grep 结果里每一份都是活的。

## Non-goals

- 不动旧页令牌键 `qiuqiu.operator.token`（属旧页，随 ADR-0011 退役一并清理——评审 Q3 已拍板）。
- 不引入跨页缓存 / React Query。
- 不做 main.go 大拆分（独立候选 server-surface-split）。

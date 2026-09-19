# Design: Quick Wins Cleanup

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | 删除以「仓库级零调用方」为硬标准，逐跳核实后再删（pipeline 包若整链死亡则整包删除）。已核实清单：pipeline.ClassifyIntent、generateProactiveText、llm 默认人格（仅 dev CLI 与测试使用）、setAccessToken（零调用）、SCOPE_TRACE_READ（零外部引用）。 |
| 2 | 身份单源：OperatorProvider 是唯一身份出口——JWT 会话在登录/刷新时解码一次缓存，机器令牌会话以 whoami 兜底；组件不再自行解码。 |
| 3 | 事件词汇表以 event-model.ts 的 definition（label + 类型全集）为单一源，衍生页引用；MatchSettings 的自动化选项由其派生。 |
| 4 | TraceEvidence module 收敛 reason 码表、citation 匹配、Drawer 三件事；线程状态标签并入 format.ts。 |
| 5 | operatorauth 具名能力 interface 与既有 Directory 并列声明；调用点改断言具名类型，双签名 Delete 探测收敛为单签名。 |
| 6 | 删除类改动不加新测试：行为未变，靠编译器 + 既有测试（go test 全量、console 构建、evals）兜底。 |

## Testing decisions

- 删除/合并的外部行为不变，验证 = `go test ./...` 绿 + console `tsc && vite build` 绿 + evals（pr 档）绿。
- 身份单源涉及可观察行为（刷新后姓名/scope 稳定）：console evals 里已有登录/身份路径的覆盖保持绿即可，不新写渲染测试。

## Further notes

- ReactRouterLink 有 ~15 处使用（评审报告写 6 处为误，已修正）：纯机械替换为 react-router 原生 Link。
- fallbackProactiveText（main.go，live 于主动装饰路径）与 agent.fallbackProactive（live 于比赛反应路径）合并方向：live 调用方改为共用 companion 导出的那一份，删除 main.go 副本。

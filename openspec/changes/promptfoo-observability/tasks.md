# Tasks: promptfoo 观测层外挂

- [x] 8.1 promptfoo 配置:tests/promptfoo/promptfooconfig.yaml(provider=exec 走 WS user_speech 真实全栈 router→policy→realizer;judge 维度=口吻/编造护栏;矩阵留尾);provider 脚本 scripts/promptfoo/realize.mjs;`npm run eval:observe` 接入(commit d33d7c1)。
- [ ] 8.2 分界验证:**结构性成立,实测验证留尾**——分界由构造保证(promptfoo 产物不进任何门禁脚本,evals/runner 不读其输出),但「judge 故意跑挂不影响门禁」与「CI 产出报告」两项 SC 未实测:本仓无 CI 工作流(evals.yml 不存在),promptfoo 需 demo server 运行中+judge LLM 端点(PROMPTFOO_LLM_* env),本机未搭;启用时先 `npx promptfoo eval` 实跑一轮并做 judge 失败注入验证,回填本项。
- [x] 8.3 矩阵留尾:启用条件(换模型/AB 需求)如实记入本 change 留尾,不预建配置。

## Implementation Notes(2026-09-30,code-review Spec 轴修正)

commit d33d7c1 时配置与脚本已落库但本清单未勾、SC 未兑现也未留尾——账实失真由审查指出后回写:8.1 勾(交付物在库),8.2 如实转留尾(CI 与 judge 实测依赖运行环境,结构性分界≠已验证)。门禁层 232 用例不受本卡任何影响(构造性分界)。

## Sequencing

波D 收尾小件,独立可最后落;不依赖其他 change。

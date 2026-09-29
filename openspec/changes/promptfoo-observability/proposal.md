# promptfoo 评测外挂:观测层,不进门禁

## Why

自建 eval runner(232+ 用例,回放型/幂等/确定性)是门禁资产,但没有 prompt×model 矩阵展开、LLM-as-judge 断言库、CI 回归 diff 报告——这些 promptfoo(MIT,OpenAI 收购后仍开源维护;本地 npm 包无云依赖,中国网络无障碍)现成。judge 是**非确定性**的,直接进门禁会污染确定性门禁哲学(flaky judge 毁门禁是行业常见事故)。

## What Changes

- promptfoo 接入为 **CI 观测层外挂**:http provider 打现有 eval 端点/脚本;报告产物进 CI artifacts。
- **两层分界立法**:judge 只用于观测层(措辞质量/口吻一致性等主观维度,出报告不挡门);门禁层永远是确定性断言——**232 用例 + pr tier 一字不动**。
- 矩阵后置:prompt×model 矩阵当前无真实对象(模型面 mimo 系,无 AB 需求),等换模型/AB 需求出现再开(届时只是 promptfoo 配置)。

## User Stories

1. As a 措辞迭代者, I want judge 报告看主观质量趋势, so that 措辞改进有观测面。
2. As a 门禁守卫者, I want judge 永不挡门, so that 确定性门禁不被 flaky 污染。

## Non-goals

- 替代/改造自建 eval runner(232 用例资产不动)。
- judge 进门禁(立法禁止)。
- 矩阵启用(后置,启用条件=换模型/AB 需求)。

## Success Criteria

- CI 跑通产出报告(观测层);
- **judge 故意跑挂不影响门禁结论**(分界验证);
- 232 门禁全绿不动。

# Console Contract Goldens: 趁 oracle 还在，锁住 console↔Go 契约

## Why

console 前端约 30 个 TS interface 是照着 Go 后端手抄的，运行期全靠 `as` 断言静默兜底——后端改个字段名，前端编译照过、测试照绿，运营员只看到 0 / — / 空名单。目前仓库唯一的契约测试（check-director-event-model.mjs）把新 console 锁向 operator-live-state.js——一个按 ADR-0011 计划注定删除的文件。退役门（28 evals + 签署 + 真机验收）一过，console 与 Go 之间就零锁定。另外 console 前端目前完全不在 CI 里，连编译错误都拦不住。

## What Changes

- Go 侧为 console 消费的全部端点建立 golden payload 快照测试：/api/matches/:id 的 events、clock、config、state 与 /api/console/ 的 overview、threads、match users、operators 等——handler 为 oracle，形状漂移即红。
- 快照存 backend 测试 testdata，含非确定字段（时间戳等）的归一化规则。
- console 构建纳入 CI（evals.yml 增加一步：npm ci + tsc && vite build）。
- 既有 event-model parity 测试保留至退役（ADR-0011 退役门依赖它）；退役时其锚点迁到 golden（任务列出，退役时执行）。

## User Stories

1. As a 后端维护者, I want 改 console 消费端点的字段名时 CI 变红, so that 破坏在合并前被拦而不是运营员看到空值。
2. As a 前端维护者, I want 对照 golden 文件更新 TS 类型, so that 手抄类型有唯一真源可对。
3. As a CI, I want console 编译错误能被拦截, so that 前端坏提交进不了 master。
4. As a 即将退役 operator.html 的团队, I want 契约锁独立于旧页存在, so that 退役不带走安全网。
5. As a 退役签署人（导演）, I want 功能对齐清单的每一项有可执行的 golden 证据, so that 签署基于事实而非记忆。
6. As a 未来 console 页面作者, I want 新端点接入时照着 golden 写类型, so that 不需要读 Go 源码反推形状。

## Non-goals

- 不做 TS 类型代码生成（评审 Q1 已拍板 golden fixtures；codegen 等出现第二消费者再议）。
- 不改任何端点的实际形状（只锁不造）。
- 不在 CI 加 Playwright 之外的新测试基建。

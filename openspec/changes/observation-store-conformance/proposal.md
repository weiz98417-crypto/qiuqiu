# Observation Store Conformance: 一套规则两份实现补上一致性证明

## Why

观察协调器 Record 的去重 / 5 条上限 / 窗口覆盖规则在内存与 Postgres 两个 adapter 里各写一遍（coordinator.go:123-150 vs postgres.go:78-101），既有测试只覆盖内存侧——生产走的 Postgres 路径没有任何测试保证它遵守同样规则，规则漂移会静默改变线上观察跟进行为。

## What Changes

- 抽取纯 reducer：applyRecordRules(observations, pending) → 规则应用结果（去重、超限淘汰、窗口覆盖），两个 adapter 的 Record 都改为调用它。
- 新增双实现 conformance 表驱动测试：同一批操作序列在内存 store 与 postgres store（DATABASE_URL 自跳过）上产出相同结果。
- Postgres adapter 行为零变化（重构即等价）。

## User Stories

1. As a 维护者, I want 规则只在一处定义, so that 两个 adapter 不可能漂移。
2. As a 生产运维者, I want Postgres 路径的规则有测试背书, so that 线上行为与测试所验证的一致。

## Non-goals

- 不改任何规则本身（去重 / 上限 / 窗口语义原样）。
- 不动 applyFact / matcher（已是共享深代码）。
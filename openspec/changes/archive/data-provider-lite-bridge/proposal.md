# Data Provider Lite Bridge

## Why

Manual operation solves the MVP blocker, but lightweight data can still improve trust and reduce operator work. The product should support schedules, teams, lineups, and score checks from cheap/free providers without becoming dependent on them.

## What Changes

- Define a provider adapter boundary for low-cost football data.
- Start with non-critical data: fixtures, teams, match metadata, and optional score validation.
- Keep provider data subordinate to operator-authored live events during the MVP.

## Non-goals

- Do not buy or integrate official live event streams in this change.
- Do not scrape copyrighted broadcast data.
- Do not make provider availability required for the app to run.

## Success Criteria

- The app can run a manual match with no provider configured.
- Provider data can prefill match metadata when available.
- Provider failures degrade gracefully.

---

## 处置记录(2026-10-04,auto-hosting 2.7)

本 change 立项于导演手工录入时代,12 项任务 0 勾。其有用范围已被 auto-hosting
(openspec/changes/auto-hosting,ADR-0024)覆盖并超额兑现:

- fixtures/赛程 → api-sports schedule reader + 订阅展开(已上线);
- 比分校验 → ESPN×api-sports 双源仲裁 + 跨源冲突隔离(auto-hosting 2.4/2.5);
- 元数据预填 → POST /api/matches/{id}/host 一键托管(ESPN summary 填充配置)。

「provider 从属运营事件」原则由 ADR-0024 信任分级确认继承(自动确认=信任表
白名单,VAR/改判类永远运营)。整目录归档,不再单独推进。

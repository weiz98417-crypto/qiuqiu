# OpenSpec Archive: 建立归档惯例，让 spec 树反映现实

## Why

openspec/changes/ 累积 51 个 change 目录零归档：22 个已全部实施、20 个有未完成项、9 个空清单。spec 树不再反映现实，新 agent 与读者的上下文为已完成的方案反复付费。归档惯例此前不存在（无 archive/ 目录、无文档提及），本变更新立该惯例。

## What Changes

- 新立 openspec/changes/archive/ 目录与惯例注记（写入 openspec/config.yaml 或 README 级说明）：tasks.md 全部勾选的 change 实施完成后整体移入 archive/ 保留历史。
- 22 个已全部实施的 change 移入 archive/（git mv 保留历史）。
- 9 个空清单 change 一并归档（无内容可丢，移走减噪）。
- 20 个有未完成项的 change 保留原地——它们仍是活工作。

## User Stories

1. As a 新 agent / 新读者, I want changes/ 只列活工作, so that 上下文不被 40 份已完成方案淹没。
2. As a 历史查阅者, I want 已实施方案在 archive/ 完整保留, so that 决策脉络可回溯。

## Non-goals

- 不判断 20 个未完成 change 的去留（它们是活工作，由各自负责人推进）。
- 不改任何 change 的内容（只移动位置）。
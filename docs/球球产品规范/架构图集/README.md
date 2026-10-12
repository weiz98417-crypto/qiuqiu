# 架构图集（archify 生成的 HTML 渲染集）

> 收录日期：2026-06-09（文档时间线）。本目录是**生成的渲染物**，不是规范。

## 这是什么

improve-codebase（archify）管线从 `architecture/archify/*.json`（15 张图的声明式源，仓库根 `architecture/` 目录，git 在管）渲染出的可浏览 HTML 架构图集。生成物曾按「生成物不入库」惯例于 fe4b3a1 移出仓库；经产品决定，渲染集作为交付物收录于此，随仓库分发。

## 清单与阅读顺序

`index.html` 为总入口。单图对应关系（图 → 规范承接篇）：

| 图 | 内容 | 承接 |
| --- | --- | --- |
| 00-overview | 全景总览 | 19 篇架构总览 |
| 01-context | 系统上下文 | 19 篇 |
| 02-containers-mvp | 容器视图（MVP 形态） | 19/23 篇 |
| 03-runtime-voice | 语音运行时 | 06/20 篇 |
| 04-runtime-match-event | 赛事事件运行时 | 14/30 篇 |
| 05-event-flow | 事件流 | 14/18 篇 |
| 06-deployment-mvp | 部署视图（MVP） | 23 篇 |
| 07-deployment-production | 部署视图（生产形态） | 23 篇 |
| 08-control-reliability | 控制与可靠性 | 21/42 篇 |
| 09-realtime-contract | 实时契约 | 18 篇 |
| 10-fact-reliability | 事实可靠性 | 30 篇 |
| 11-delivery-assurance | 投递保障 | 21 篇 |
| agent-core | Agent 核心 | 01/07 篇 |
| technical-layers / technical-modules | 技术分层与模块 | 19 篇 |

## 时效声明

本批 HTML 渲染自 2026-05-16 前后的 archify 源，**晚于它的能力波次（自动确认双通道、TTS 三态、smart-turn sidecar、危机分级等）可能未反映在图中**；架构事实以 19 篇《架构总览》与 `architecture/archify/*.json` 为准，本目录是可视化参照。更新方式：改 JSON 源 → 重跑 archify 渲染 → 覆盖本目录并在此登记修订。

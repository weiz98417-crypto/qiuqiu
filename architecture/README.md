# 球球架构图集

这组图分成三层：先用产品级视角讲清楚系统阶段、Agent 职责和逻辑技术分层，再用技术模块图解释真实运行边界，最后用 C4、时序、数据流和部署图下钻到工程实现。

## 入口

- 直接打开 [`index.html`](index.html) 进入图集目录。
- 需要按顺序打开全部 15 张图时，打开 [`showcase/index.html`](showcase/index.html)。
- 需要单张图的交互追踪、搜索和演示模式时，打开 [`showcase/00-overview.html`](showcase/00-overview.html)。
- 需要代码评审或文档嵌入时，使用 `showcase/` 中对应图的 SVG 导出或截图。

## 阅读顺序

先读 00 总图，再读 A1 Agent Core、A2-L 技术架构分层、A2-M 技术模块地图和 A3 控制面与可靠性；这五张图分别对应 5 段主链、真实决策闭环、逻辑层级、运行边界矩阵和可靠性控制闭环。之后沿工程图继续下钻。

| 顺序 | 图 | 读者 | 事实源 |
| --- | --- | --- | --- |
| 00 | 全链路总图 | 全员 | `archify/00-overview.json` |
| A1 | Agent Core 决策闭环 | Agent、后端、评测 | `archify/agent-core.json` + `backend/internal/companion/agent.go` |
| A2-L | 技术架构分层（逻辑职责） | 学习者、架构、研发 | `archify/technical-layers.json` |
| A2-M | 技术模块地图 | 学习者、架构、研发 | `archify/technical-modules.json` + 宣传稿模块矩阵 |
| A3 | 控制面与可靠性 | 后端、运维、安全、评测 | `archify/08-control-reliability.json` |
| 01 | 系统上下文 | 产品、架构、合作方 | `archify/01-context.json` |
| 02 | MVP 容器 | 客户端、后端、运维 | `archify/02-containers-mvp.json` |
| 03 | 用户语音时序 | 客户端、后端、AI | `archify/03-runtime-voice.json` |
| 04 | 比赛事件时序 | 事件、AI、运营 | `archify/04-runtime-match-event.json` |
| 05 | 事件与数据流 | 后端、数据、可靠性 | `archify/05-event-flow.json` |
| 06 | MVP 部署 | 开发、运维 | `archify/06-deployment-mvp.json` |
| 07 | 生产部署目标 | 运维、安全、成本评审 | `archify/07-deployment-production.json` |
| 09 | 实时接口与投递契约 | 客户端、后端、协议评审 | `archify/09-realtime-contract.json` |
| 10 | 事实与可靠性状态 | 事实、消息、恢复评审 | `archify/10-fact-reliability.json` |
| 11 | 表现、评测与运维闭环 | 客户端、评测、运维 | `archify/11-delivery-assurance.json` |

## 维护规则

1. C4 静态层级和部署事实维护在 `structurizr/workspace.dsl`。
2. 总图、Agent Core、技术模块图、时序图和数据流维护在 `archify/*.json`。
3. HTML 是 Archify `deliver` 生成物，不手工修改。
4. “逻辑职责”表示当前代码中的责任边界，不暗示独立进程；`planned` 节点代表路线图，不代表当前已部署能力。
5. Redis 在图中表示会话、短期状态和协调；它不是事实源。PostgreSQL + Outbox 才是可追溯事实和事务消息底座。
6. 09–11 是工程合同级补充视图：09 约定实时消息怎么传，10 约定事实和失败如何变状态，11 约定表现结果如何回执并进入评测和收缩。它们不替代 01–07 的边界、时序和部署图。
7. 代码、配置或部署发生变化时，先更新源模型，再重新校验和交付。

## 视觉约束

图集沿用球球设计系统的夜场转播语气：`#070B12` 夜空、暖白文字、橙色主链路、绿色事实层。颜色不是唯一语义，线型和文字同时表达当前态、异步态与规划态。

阅读尺度以 05 事件与数据流图为基准：03、04 两张时序图保持全链路首屏可见，02、06 两张 MVP 工程图收紧横向画布，避免节点文字因留白过宽而缩小。

尺寸说明：`1440×900` 与 `1920×1080` 是浏览器验收视口，不是 HTML 图的固定像素尺寸。Archify 使用逻辑 `viewBox` 配合响应式 Viewer，同一张图可以在 1920×1080 屏幕展示；若把逻辑画布直接扩大到 1920，节点和文字会按比例缩小。需要演示或 PPT 素材时，在 Viewer 的导出菜单生成整图 PNG/SVG，或在 1920×1080 浏览器视口截图。

# Phase 0 — 基础搭建

## 目标

验证三个核心技术假设 + 搭好脚手架，确认技术方案可行后进入 Phase 1 MVP 开发。

## 范围

| 模块 | 内容 | 交付物 |
|------|------|--------|
| Go 后端脚手架 | WebSocket 服务、Docker Compose | 可运行 API 服务 |
| Flutter 客户端脚手架 | WebView 页面、基础 UI | 可运行 App |
| 数据源验证 | api-sports.io 联调 | 确认数据可用 |
| Live2D 验证 | WebView + Cubism Web SDK | 角色可渲染 |
| LLM+TTS 延迟测试 | 串联调用，测端到端延迟 | 延迟数据 |

## 不在范围

- 事件处理引擎（Phase 1）
- LLM 对话生成（Phase 1）
- ASR 语音识别（Phase 2）
- 用户系统（Phase 4）

## 技术决策（Explore 阶段已拍板）

1. 消息队列：Phase 0-3 用 Go channel / Redis pub/sub，NATS 留到 Phase 4
2. Live2D：WebView + Cubism Web SDK（最快验证路径）
3. 延迟预算：LLM <1s + TTS <800ms + 网络 <200ms，目标总 <2s

## 里程碑 M0

- [ ] Go 服务可接收 WebSocket 连接
- [ ] Flutter App 可在模拟器/真机上运行
- [ ] api-sports.io 可拉取一场真实比赛的实时事件
- [ ] Live2D 角色模型可在 Flutter WebView 中渲染
- [ ] LLM + TTS 串联延迟 < 3s（若 >3s 触发 Phase 0 闸门回退）

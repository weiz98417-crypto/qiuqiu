# Phase 0 任务拆解

## 任务列表

### T1: Go 后端脚手架 ✅
- **目标**: 可运行的 WebSocket 服务 + Docker Compose
- **内容**:
  - ✅ `go mod init` + 项目目录结构
  - ✅ `/health` HTTP 端点
  - ✅ `/ws/match/{match_id}` WebSocket 端点（握手+心跳+token认证+IP限流）
  - ✅ Docker Compose（Go + Redis）
- **验证**: ✅ `go build` 成功

### T2: Flutter 客户端脚手架 ✅
- **目标**: 可运行的 App + WebView 页面
- **内容**:
  - ✅ 项目目录结构（lib/screens/, widgets/, services/）
  - ✅ `MatchScreen` 页面（比分栏 + Live2D区 + 控制栏）
  - ✅ `flutter_inappwebview` 配置（Live2dView Widget）
  - ✅ WebSocket 连接基础代码
- **验证**: 代码就绪（flutter run 需 Flutter SDK，用户本地执行）

### T3: 数据源验证 ✅
- **目标**: 确认 api-sports.io 数据可用
- **内容**:
  - ✅ api-sports.io HTTP client 封装（datasource/apisports.go）
  - ✅ 比赛列表/实时比赛/事件 三个端点
  - ✅ verify-datasource CLI 工具（build 通过）
  - ⏳ 运行需 APISPORTS_API_KEY（用户自行填入 .env 后执行）
- **验证**: ✅ `go build` 成功

### T4: Live2D WebView 验证 ✅
- **目标**: 角色模型在 Flutter WebView 中可渲染
- **内容**:
  - ✅ Live2D 示例模型目录结构（Haru placeholder）
  - ✅ index.html — Canvas 占位渲染 + Flutter Bridge 回调
  - ✅ setExpression / setSpeaking JS 桥
  - ✅ Lip Sync RMS 循环（100ms 间隔，WebView 内闭环）
  - ✅ Flutter `InAppWebView` 加载本地 HTML（Live2dView Widget）
  - ⏳ 升级到 Cubism SDK 渲染需下载官方 SDK（Phase 1 live2d-character）
- **验证**: ✅ 代码就绪，WebView 加载路径配置完成

### T5: LLM + TTS 延迟测试 ✅
- **目标**: 端到端延迟数据
- **内容**:
  - ✅ LLM client (Deepseek API, 5 类事件场景)
  - ✅ TTS client (ElevenLabs API)
  - ✅ latency-test CLI（LLM+TTS 串联 + p50/p95 统计 + 闸门判定）
- **验证**: ✅ `go build` 成功（运行需 API Key）

---

## 执行顺序

```
T1 ──▶ T3 ──▶ T5
  │
  └──▶ T2 ──▶ T4

T1 和 T2 可并行
T3 依赖 T1（用 Go 代码验证）
T5 依赖 T3（用真实事件驱动）
T4 依赖 T2（用 Flutter 验证）
```

## 闸门检查

Phase 0 完成后，在进入 Phase 1 前检查：

- [ ] LLM + TTS 延迟 < 3s（T5 验证）— 代码就绪，运行 `latency-test.exe` 需 API Key
- [ ] api-sports.io 数据可用（T3 验证）— 代码就绪，运行 `verify-datasource.exe` 需 API Key
- [ ] Live2D WebView 可渲染（T4 验证）— 代码就绪，运行 `flutter run` 需 Flutter SDK

**任一失败 → 回退方案A（纯语音+文字，推迟 Live2D 和 ASR）**

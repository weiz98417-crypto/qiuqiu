# Live2D Character 技术设计

## 1. 架构

```
Flutter (Dart)                         WebView (JS)
┌──────────────────────┐     ┌───────────────────────────┐
│  Live2dWidget        │     │  index.html               │
│  ┌────────────────┐  │     │  ┌─────────────────────┐  │
│  │ InAppWebView   │◀─┼──▶──┼──│ Canvas #live2d       │  │
│  │                │  │     │  │ (WebGL2 渲染)       │  │
│  │                │  │     │  │                     │  │
│  │ load local     │  │     │  │ Cubism SDK for Web  │  │
│  │ assets/html    │  │     │  │ live2d-renderer     │  │
│  └────────────────┘  │     │  └─────────────────────┘  │
│                      │     │                           │
│  evaluateJavascript()│     │  window.flutterCallback()  │
│  ──────────────────▶ │     │  ◀─────────────────────   │
│                      │     │                           │
│  onMessage callback  │     │  postMessage()            │
│  ◀────────────────── │     │  ─────────────────────▶   │
└──────────────────────┘     └───────────────────────────┘
```

## 2. WebView 选型

使用 `flutter_inappwebview`（非 webview_flutter）：
- 功能最全，支持 JS Bridge
- `InAppWebView.evaluateJavascript()` 同步调用
- `InAppWebView.onMessage` 接收 JS 回调
- 支持加载本地 HTML（`InAppWebView.initialData` 或本地 server）

## 3. 本地资源结构

```
client/assets/live2d/
├── index.html               # 加载入口
├── cubismcore/              # Cubism 5 Web Core JS
├── live2d-renderer-lite.js  # 社区封装库
└── models/
    └── Haru/                # 示例模型（Phase 0 验证用）
        ├── Haru.model3.json
        ├── Haru.moc3
        └── textures/
```

## 4. JS Bridge 协议

```dart
// Dart → JS: 表情切换（低频）
webView.evaluateJavascript(data: "setExpression('excited')");

// Dart → JS: 说话状态切换（说话开始时调用，结束时调用）
webView.evaluateJavascript(data: "setSpeaking(true)");
webView.evaluateJavascript(data: "setSpeaking(false)");

// JS → Dart: 模型加载完成
webView.onMessage.listen((msg) {
  if (msg.data['type'] == 'ready') { /* 模型就绪 */ }
});
```

```javascript
// JS 侧
function setExpression(name) {
    cubismModel.setExpression(name);  // excited|nervous|normal|tease|regret
}

function setMouthOpen(value) {
    cubismModel.setParameter('ParamMouthOpenY', value);  // 0.0 ~ 1.0
}

// 加载完成后通知 Flutter
window.addEventListener('modelReady', () => {
    window.flutter_inappwebview.callHandler('onModelReady');
});
```

## 5. Lip Sync 算法

> **架构决策**：音频播放和 RMS 计算在 WebView 内闭环，使用 Web Audio API。
> Dart 侧不传递音频帧，仅发送高层指令（speaking/stopped），避免高频 Dart-JS Bridge 通信。

```
WebView 内闭环：
  TTS PCM 数据 → Web Audio API 播放
      │
      ▼ 每 100ms 取一帧 RMS
  AnalyserNode.getByteTimeDomainData()
      │
      ▼ 归一化
  normalized = min(rms / threshold, 1.0)
      │
      ▼ 低通滤波
  smoothed = prev * 0.7 + normalized * 0.3
      │
      ▼ 直接设置 Live2D 参数
  ParamMouthOpenY = clamp(smoothed, 0.0, 1.0)

Dart 侧仅发送：
  - setSpeaking(true/false)  — 开始/停止说话
  - setExpression(name)      — 表情切换（低频）
```

**性能权衡**：100ms 更新率（10Hz）在感知上几乎无差异，但 Dart-JS Bridge 调用降为每 100ms 一次（仅为原来的 1/5），大幅减少跨线程通信开销和电池消耗。

## 6. 表情过渡

```
表情切换不硬切，使用 ease-out 过渡：

idle ──(200ms ease-out)──▶ excited
excited ──(500ms ease-out)──▶ idle
nervous ──(150ms ease-out)──▶ tease
```

在 WebView 侧使用 `requestAnimationFrame` 做插值过渡。

## 7. Live2dWidget Flutter 封装

```dart
class Live2dWidget extends StatefulWidget {
  final String expression;       // 当前表情（外部驱动）
  final bool isSpeaking;         // 是否正在说话

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: MediaQuery.of(context).size.height * 0.6,
      child: InAppWebView(
        initialData: InAppWebViewInitialData(data: loadLocalHTML()),
        onWebViewCreated: (controller) {
          controller.onMessage.listen(_handleJSMessage);
        },
        initialSettings: InAppWebViewSettings(
          isTransparentBackground: true,
          javaScriptEnabled: true,
          disableDefaultErrorPage: true,
          // 安全：禁止网络访问（仅本地 assets）
          contentBlockers: [],
        ),
      ),
    );
  }
}
```

## 8. 性能目标

| 指标 | 目标 |
|------|------|
| 模型首帧加载 | < 500ms |
| 模型内存 | < 30MB |
| 运行帧率 | ≥ 30fps（低端机） |
| Lip Sync 延迟 | < 50ms |

# Live2D Character 任务拆解

### T1: WebView + Cubism Web SDK 加载
- 准备 Live2D 示例模型（Haru 或 Hiyori）
- 编写 HTML 入口页（Canvas + live2d-renderer-lite）
- Flutter 侧 InAppWebView 加载本地 HTML
- **验证**: 手机屏幕上看到 Live2D 角色

### T2: Dart ↔ JS 通信桥
- Dart `evaluateJavascript()` 封装
- JS `postMessage()` → Dart 回调
- 模型加载完成通知
- **验证**: Dart 侧收到 "modelReady" 回调

### T3: 表情切换
- JS 侧实现 `setExpression(name)`
- 5 种表情映射（idle/excited/nervous/tease/regret）
- 表情过渡动画（ease-out）
- Dart 侧 `Live2dWidget` 响应 expression 属性变化
- **验证**: 手动切换表情，角色表情平滑过渡

### T4: Lip Sync 嘴型同步（WebView 内闭环）
- JS 侧 Web Audio API `AnalyserNode` 获取 RMS（每 100ms）
- 音量 → ParamMouthOpenY 映射 + clamp(0.0, 1.0)
- 低通滤波平滑（prev*0.7 + new*0.3）
- Dart 侧仅发送 `setSpeaking(true/false)`，不传输音频帧
- **验证**: 模拟音频在 WebView 内播放，角色嘴巴跟随音量开合
- **注意**: 100ms 更新率在感知上与 20ms 无差异，但 Dart-JS Bridge 调用减少 5×

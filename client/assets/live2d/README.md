# Live2D Assets Setup

Phase 0 使用 Canvas 占位渲染验证 WebView 可用性。
Phase 1 (live2d-character) 替换为 Cubism 5 Web SDK 完整集成。

## 升级到 Cubism SDK 的步骤

1. 从 https://www.live2d.com/en/download/cubism-sdk/ 下载 Cubism 5 SDK for Web
2. 将 `live2dcubismcore.min.js` 放入 `cubismcore/`
3. 将模型文件（.model3.json, .moc3, textures/）放入 `models/Haru/`
4. 替换 `index.html` 中的 placeholder 渲染为 Cubism SDK WebGL 渲染

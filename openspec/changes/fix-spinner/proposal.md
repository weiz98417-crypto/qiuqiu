# fix-spinner — Live2D 加载后转圈不消失

## 问题

Web 端 InAppWebView 多个 API 不支持（addJavaScriptHandler, onLoadResourceCustomScheme, onConsoleMessage），导致 Flutter 端无法感知 Live2D 模型加载完成，`_modelReady` 永远为 false，加载转圈一直显示。

## 修复

`live2d_view.dart`: `onLoadStop` 后启动 5 秒超时兜底——超时后直接 `setState(_modelReady = true)`，不等 JS 通信。

## 影响

- 文件: `client/lib/widgets/live2d_view.dart`
- 改动: ~5 行
- 不新增依赖

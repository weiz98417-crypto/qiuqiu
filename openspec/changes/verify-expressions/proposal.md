# verify-expressions — 验证 Live2D 表情映射

## 问题

当前 exprMap 映射是猜的:
```js
{ idle:0, listening:0, confused:0, excited:1, chat:3, tease:3, happy:3, nervous:4, sad:4, surprised:5, angry:6 }
```

7 个 `.exp3.json` 文件的实际表情效果未知。后端发了 `excited` 但 Live2D 可能显示哭脸。

## 做法

1. 创建 `test-expressions.html` 测试页，Go 后端托管
2. 页面加载相同模型，加 7 个按钮逐个播放 EXP0~EXP6
3. 用户打开浏览器 → 逐个点击 → 记录每个表情的实际视觉效果
4. 根据实际效果修正 exprMap

## 文件

- 新建: `client/assets/live2d/test-expressions.html`
- 修改: `live2d.html`, `live2d_view.dart` 中的 exprMap

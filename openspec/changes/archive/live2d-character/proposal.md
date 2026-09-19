# Live2D Character — 角色展示系统

## 目标

在 Flutter App 中集成 Live2D 角色渲染，实现待机动画、表情切换、嘴型同步（Lip Sync），让球球有"身体"。

## 范围

| 模块 | 内容 |
|------|------|
| WebView 集成 | flutter_inappwebview 加载本地 HTML + Cubism Web SDK |
| 角色渲染 | 加载 Live2D 示例模型（Haru），Canvas WebGL2 渲染 |
| 表情切换 | 5 种表情（idle/excited/nervous/tease/regret）平滑过渡 |
| Lip Sync | 音频音量 → 嘴型参数映射，20ms 采样窗口 |
| Flutter ↔ JS Bridge | Dart 侧通过 evaluateJavascript / postMessage 通信 |

## 不在范围

- 球球专属角色模型（用示例模型验证，定制模型后续）
- 用户语音交互（Phase 2）
- 动作触发系统（Phase 2）

## 依赖

- `phase-0-foundation` — Flutter 脚手架

## 里程碑

- [ ] Live2D 模型在手机屏幕上渲染
- [ ] 待机状态有呼吸动画
- [ ] Dart 发指令 → JS 切换表情成功
- [ ] 模拟音频输入 → 嘴型跟随变化

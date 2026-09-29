# Live2D 引擎基底切换:advanced fork + 末帧保持 + 变体轮转

## Why

渲染引擎依赖链断供:qiuqiu fork → RaSan87/pixi-live2d-display-lipsyncpatch(**404 已消失**)→ guansss/pixi-live2d-display(2024-08 起休眠)——上游死后 bug 只能自持自修、无社区反哺。pixi-live2d-display-advanced(MIT,活跃,同 PixiJS v7 血统)提供 parallelMotion / **parallelLastFrame(动作末帧保持——「进球庆祝定格」场景即插即用)** / 更优动作预约。0ccd9f0 可复现构建管线(npmmirror+esbuild+哈希)复用只换源包。口型 wLipSync MFCC→visemes 已就位(live2d_view.dart:333-364,vendored)不动。另一增量:指令性 motion 永远同一变体(idle 已有 8-15s 随机轮播,指令没有)——机械感。

## What Changes

- **基底切换**:pixi 7.4.3 legacy 之上的库换为 pixi-live2d-display-advanced;qiuqiu fork 薄化(只留 qiuqiu 特有适配:alias shim / wLipSync 桥 / presentation-map 解析);构建管线复用、哈希重锁。
- **末帧保持**:presentation-map acts 表加 `holdLastFrame` 可选槽(三方镜像 Dart==JSON==Go 同步,契约测试扩展);与 ReturnMode 正交——表情归位照旧,末帧由 ReturnMode 归位或新 motion 抢占时清;先开庆祝类/懊恼类 act,逐 act 审核开。ADR-0007 修订注随附。
- **变体轮转(服务端)**:同一语义槽的变体池服务端轮转选(复用 backchannel 短语轮转模式);契约零改动——每个变体已有白名单名(presentation_state.dart allowedMotions 含 17 motions 全量);trace/审计可见实际选择。SillyTavern allowMultiple 重掷思想(AGPL,只按文档抄思想,不碰代码)。
- **parallelMotion 留槽不投产**:acts 表 `parallel` 可选槽,默认关——现模型 motion 组多为组合动作,独立并行需求未证实;启用时验证 motion 曲线与 wLipSync 嘴参数冲突(若曲线含嘴参数)。
- 表情 index 0 空文件坑由既有守卫测试继续锁;模型资产不换(female_01Arkit_6)。

## User Stories

1. As a 用户, I want 进球庆祝定格在高潮姿势, so that 情绪表达完整不落空。
2. As a 用户, I want 同样的庆祝每次动作略不同, so that 不机械。
3. As a 维护者, I want 引擎上游活着, so that bug 有社区反哺、fork 自持面积最小。

## Non-goals

- parallelMotion 投产(留槽)。
- 模型/表情资产更换、motion 资产扩充(独立;表情 index 0 坑由守卫锁)。
- 口型升级(wLipSync 保留,advanced 自带口型禁用)。
- Cubism 官方 SDK 迁移(Open-LLM-VTuber 路线,重大重构,留观不立项)。

## Success Criteria

- 双渲染面(web assets/live2d/live2d.html + Flutter 内嵌页)Chrome 像素验证(基线截图对比,波A 先例);
- 契约测试三方镜像扩展全绿;17 motion 全量触发回归;
- idle 轮播行为回归(8-15s 随机+说话让路+6s 指令让路逻辑不丢,live2d_view.dart:705-719);
- **web 构建绿**(9b6e1f0 教训:换库最易打坏 web);
- 变体轮转 eval:同语义槽连续触发变体不重复(轮转语义)+trace 记录实际变体。

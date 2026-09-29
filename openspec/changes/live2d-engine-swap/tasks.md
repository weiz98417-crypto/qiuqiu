# Tasks: Live2D 引擎基底切换

- [ ] 6.1 构建管线:源包换 pixi-live2d-display-advanced(npmmirror 锁版本);bundle 重构建+哈希重锁;fork 薄化(只留 alias shim/wLipSync 桥/presentation-map 解析适配层)。
- [ ] 6.2 双渲染面切换+回归:两个渲染面的适配(live2d.html 与 Flutter 内嵌 JS 各自 alias shim 不变,底层库换);基线截图重拍+像素验证;17 motion 全量回归;idle 轮播回归;web 构建绿;**advanced 自带口型禁用**(wLipSync 保留)。
- [ ] 6.3 末帧保持:acts `holdLastFrame` 槽(三方镜像+契约测试);庆祝类/懊恼类 act 开;ReturnMode 归位交互用例(末帧由归位或新 motion 抢占清)。
- [ ] 6.4 变体轮转:服务端变体池+轮转(短语轮转模式复用);trace 记实际变体;eval 轮转语义用例(同槽连续不重复)。
- [ ] 6.5 parallel 留槽 + ADR-0007 修订注 + 门禁:acts `parallel` 槽默认关;ADR-0007 修订注(holdLastFrame/变体轮转入表现契约);flutter test、dart analyze 零新增、web 构建、pr tier。

## Sequencing

波D。纯客户端;与 voice-streaming-delivery 在 client 侧交叉(live2d_view.dart 音频消费/FIFO),错峰:**①先③后**。与 wake-word-kws 也都在 client 侧,同样错峰。

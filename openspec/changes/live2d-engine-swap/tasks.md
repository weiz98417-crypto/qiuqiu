# Tasks: Live2D 引擎基底切换

- [x] 6.1 构建管线:源包换 pixi-live2d-display-advanced(npmmirror 锁版本);bundle 重构建+哈希重锁;fork 薄化(只留 alias shim/wLipSync 桥/presentation-map 解析适配层)。
  - package.json/lock 换 `pixi-live2d-display-advanced@1.1.0`(npmmirror,依赖 @pixi/sound 5.2.3,pixi.js 7.4.3 单副本 dedupe);src/index.js 换 `/cubism4` ESM 入口;产物 757,569B,sha256 `e0314b1a8e33cf314be9b78613b06286e40844766fa8337685e403f8ab3df5aa`,两次构建一致;上游 lipsyncpatch 依赖全部退役。
- [x] 6.2 双渲染面切换+回归:两个渲染面的适配(live2d.html 与 Flutter 内嵌 JS 各自 alias shim 不变,底层库换);基线截图重拍+像素验证;17 motion 全量回归;idle 轮播回归;web 构建绿;**advanced 自带口型禁用**(wLipSync 保留)。
  - 两渲染面 load 后 `internalModel.lipSync=false`(脚本断言其===false);口型链路(wLipSync MFCC→visemes→参数)零改动;17 motion 逐个 playMotion 全部无 console/page error,截图 17/17 非空(`artifacts/live2d-regression/`,Chrome WebGL);持帧 apply 冒烟干净;idle 轮播/让路逻辑未动(`check-live2d-idle-yield` 绿);`flutter build web` 绿(9b6e1f0 教训项)。基线对比无旧引擎截图库可依,以「渲染非空+姿势差异+零报错」代替(见 09-celebrate 与 hold-last-frame 定格姿势差异)。
- [x] 6.3 末帧保持:acts `holdLastFrame` 槽(三方镜像+契约测试);庆祝类/懊恼类 act 开;ReturnMode 归位交互用例(末帧由归位或新 motion 抢占清)。
  - JSON acts.ActReact `holdLastFrame: ["positive","negative"]`;Dart `CompanionPresentation.actsHoldLastFrame`+fromReplyData wire 解析+PresentationMap 解析;Go `presentationRow.holdLastFrame`+`Plan.HoldLastFrame` wire 字段;两侧契约测试互锁(含象限级双向);motion 全 Loop→持帧由时长定时器触发 `motionLastFrame`,holdToken 防陈旧冻结;hold 槽行 HoldMS 下限 7400ms(对 motion3.json Meta.Duration 锁,保证动作播完+定格窗);归位/抢占清帧用例:controller 侧(returnPresentation 落 resting、新 apply 顶替)+Go 侧(base/interrupted 行不持帧)。
- [x] 6.4 变体轮转:服务端变体池+轮转(短语轮转模式复用);trace 记实际变体;eval 轮转语义用例(同槽连续不重复)。
  - `relationship/presentation_variant_rotation.go`(pool[rotation%len],speak/celebrate/idle 三池;presentationFor 保持纯函数,轮转在 Director 两处组装点施加);reason code `motion_variant_rotated`+Decision.Presentation.Motion 即实际变体(trace/interaction audit 均携带);轮转语义 Go 用例(池契约/连续不重复/未知槽穿透/语义槽不漂移/Director 集成)+eval 用例 `TestEvalPresentationVariantRotationDoesNotRepeatConsecutively`(双进球庆祝连续不重复+trace 记录)。勘察结论:advanced parallelMotion 是并行组合 API、非变体调度,故轮转放服务端(客户端 web 面随机选不可审计、内嵌面固定取首个)。
- [x] 6.5 parallel 留槽 + ADR-0007 修订注 + 门禁:acts `parallel` 槽默认关;ADR-0007 修订注(holdLastFrame/变体轮转入表现契约);flutter test、dart analyze 零新增、web 构建、pr tier。
  - parallelMotion API 已在 bundle 中(baseline 可用)但 acts 无 parallel 槽、无调用点——留槽不投产(proposal 本就留槽);ADR-0007 修订注已附(holdLastFrame+变体轮转);门禁:`go test ./...` 全绿、`flutter test` 217 全绿(首跑 4 例环境性 flake,复跑两次全绿)、`dart analyze` 零新增(仅既有 4 条 info,均在未动文件)、`flutter build web` 绿、`check-presentation-map` 绿。

## Sequencing

波D。纯客户端;与 voice-streaming-delivery 在 client 侧交叉(live2d_view.dart 音频消费/FIFO),错峰:**①先③后**。与 wake-word-kws 也都在 client 侧,同样错峰。

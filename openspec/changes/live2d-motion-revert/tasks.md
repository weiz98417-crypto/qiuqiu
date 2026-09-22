# Tasks: Live2D Motion Revert

- [ ] 1.1 删 8 个自建 motion 文件；model3.json 动作组回归原装 5 组。
- [ ] 1.2 presentation-map.json 三段重拴（语义名保留、落原装，按 design 映射表）。
- [ ] 1.3 backchannel 表现表并入 presentation_table（函数注入防包环）；var_check 语义裁剪。
- [ ] 1.4 词表白名单测试 + presentation_vocabulary + 受影响 evals/golden 更新。
- [ ] 1.5 live2d-motion-pack git mv 入 archive + 取消注记。
- [ ] 1.6 验证：go test + flutter test + evals 全绿。

## Sequencing

架构评审 B 项；可与 A 并行。是 H（idle-life-signals）的前置（idle 轮播依赖原装 idle 组确认）。

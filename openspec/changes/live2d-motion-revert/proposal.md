# Live2D Motion Revert: 动作回归原装 + 表现映射单源化

## Why

2026-06-01 批次自建的 8 个动作（celebrate×2/miss/complain/tense/analysis/nod/wave，openspec live2d-motion-pack）质量未达皮套原装水平；约稿/AI 生成/换皮套三条补齐路径均被否（2026-09-23 用户裁决：无成熟 AI 工具、不约稿、原装够用）。原装 9 个动作（hello/idle×3/Listen×2/Speak×2/Think）恰好是 Turn Phase 的相位语义——动作管相位、7 个原装表情文件管情绪，与 CONTEXT.md「Turn Phase 词条 idle/听/说/想归客户端」对齐。另：backchannel.go 硬编码的 presentations 表与 presentation-map.json events 段双源漂移（var_check 两边 `thinking/analysis` vs `tense/tense`），违反 ADR-0005 单一契约。

## What Changes

- 删 8 个自建 motion 文件；`female_01Arkit_6.model3.json` 动作组回归原装 5 组（hello/idle/listen/speak/think）。
- `presentation-map.json` motions/acts/events **语义名全部保留**（celebrate/miss/complain/tense/analysis/agree/wave），映射指向原装 9 个文件——将来换资产=改 json 零代码（数据级钩子，ADR-0020 同思路）。
- 双源收敛：backchannel 的 4 条表现并入 `relationship/presentation_table.go` 单一来源，backchannel 改读共享表；`var_check` 语义裁剪：微反应场景取 `tense/tense`，分析向（thinking/analysis）留给 knowledge/问答路径。
- 词表白名单测试（`ClientAcceptsExpression/Motion`）与 presentation_vocabulary 同步；受影响 evals/golden 更新。
- `openspec/changes/live2d-motion-pack` git mv 入 `archive/`，proposal 顶部加取消注记（理由：自建动作撤回、映射回归相位+表情分工）。

## User Stories

1. As a 用户, I want 球球的动作自然不生硬, so that 陪伴不出戏。
2. As a 开发, I want 表现映射只有一份来源, so that 不会再漂移。

## Non-goals

- 任何新动作资产（约稿/AI/换皮套均否）；语义动作名收缩（保留钩子）；闲置生命感四件套（`idle-life-signals` 另立）。

## Success Criteria

- 全量 go test + flutter test + evals 绿；词表测试锁新映射；backchannel 不再持有独立表现表；motion-pack 归档带注记。

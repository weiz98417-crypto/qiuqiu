# 实战导演页对齐清单（director-rewrite task 5.1 草稿）

> 状态：**草稿，待导演（用户）逐项签核**。签核通过后才进入退役波
> （task 5.2 真机彩排 → 5.3 导航翻转与删除 operator.html）。
> 新页面入口：`/console/#/console/match/:matchId/director`（导航「新版实战导演」）。

## 已对齐（新页面已实现，建议签核 ✅）

| 老 #live 能力 | 新组件 | 说明 |
| --- | --- | --- |
| 18 事件定义（roles/actions/intensity/scoreDelta/对手位/多参与位） | `console/src/director/event-model.ts` | 49 项与老文件对拍全等（`scripts/check-director-event-model.mjs`，已入 pr 档） |
| 草稿状态机（createDraft/updateDraft/applyVoiceDraft/applyVoiceConflict/validateDraft/toEventPayload） | 同上 | 场景批：进球/比分更正/校验错误/工具函数全对拍 |
| 行为按钮按组展示、点击写草稿（draft-only） | `BehaviorBar.tsx` | 组序与老页面一致；按钮绝不直接发布 |
| 草稿卡：事件时间 / 强度 / 事实状态（候选/确认） / 推荐动作 / 球球处理 auto-quiet-manual / 事件描述 | `DraftCard.tsx` | 球球处理切换即时渲染人工话术输入框 |
| 比分更正：目标比分 + 更正原因 + 不得与当前比分相同 | `DraftCard.tsx` | 发布走 `POST /events/:id/correct`；评分规则与老页面一致 |
| 模板（45 条描述模板，按当前行为过滤）+ 发送预览 | `DraftCard.tsx` | 模板表逐条移植 |
| 暂存为候选（provisional）/ 确认并发送 | `DraftCard.tsx` | 线上事实状态 provisional / confirmed，幂等键写入 |
| 语音录入：麦克风选择 → 采集 → 16k WAV → `drafts/voice` → 转写 → 结构化草稿 + 冲突卡 → `drafts/voice/publish` | `VoiceDraft.tsx` | 采集/重采样/WAV 编码/音量指标逐函数移植；撤回本次语音保留 |
| 事实时间线：时钟 / 队伍 / 描述 / 参与人 / 上报 vs 生效比分 / 更正原因 / 状态徽标 | `FactTimeline.tsx`（antd Timeline） | 「球球主动说：」行按事实状态着色 |
| 事实动作：确认 / 撤销 / 拉回更正（载入草稿以更正发布） | `FactTimeline.tsx` + `DirectorLive.tsx` | 拉回更正保留 revisionOf 与更正原因必填 |
| 开放冲突置顶：候选/已采用选择 → `conflicts/:id/resolve` | `FactTimeline.tsx` | |
| 比分条 + 主时钟：开始/暂停 / ±10 秒 / 阶段切换 / 时钟版本 | `DirectorLive.tsx` 顶栏 | `PATCH /clock` 同一命令形状（版本冲突自动重读） |
| 名单选人：主/客队切换 + 号码姓名搜索 + 球员 chips 写参与人 | `DirectorLive.tsx` | 名单来自 `/config`（赛前配置中心已迁移） |

## 明确未搬（等待导演裁决：搬或放弃）

| 老 #live 能力 | 现状 | 建议 |
| --- | --- | --- |
| 赛前信息配置中心（setup 视图：元数据/名单文本框） | 未搬——React 比赛 page 已有保存阵容入口 | 用 console 比赛 page 替代，建议放弃 |
| 「补一句提醒」（hint 生成一句话术） | 未搬 | 待导演确认是否常用；常用则搬入 DraftCard |
| 快速组合（quickCombinations 一键多参与人预设） | 未搬 | 同上 |
| 监控视图（sources/automation/monitor/traces 四视图） | 未搬——已在 React console 各页落地（数据源与人工接管、自动化策略、引用审计/轨迹） | 无需搬运 |
| 语音设备偏好持久化（记住上次麦克风） | 已实现设备列表与 USB 优先，偏好持久化未做 | 小改动，待签核后补 |
| VAR/进球取消的 revisionOf 下拉选择原事实 | 部分对齐：校验已强制 revisionOf，但原事实下拉选择器未做 UI（当前经「拉回更正」自然携带） | 待导演确认交互 |

## 签核

- [ ] 导演签核（逐项确认上表 ✅ 区与「明确未搬」区）
- [ ] 真机直播彩排完成（task 5.2）
- [ ] 导航默认翻转 + operator.html 退役（task 5.3，签核后执行）

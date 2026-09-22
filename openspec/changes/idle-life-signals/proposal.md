# Idle Life Signals: 闲置生命感四件套

## Why

闲置时模型完全静止：无 idle 轮播（`resolveMotion` 只响应指令、变体永远取第一个）、无自动眨眼、无呼吸驱动（全仓零命中）——用户沉默看球的 90 分钟里「她不在看球」。进球反应已由 backchannel 覆盖（live2d-motion-revert 重拴后仍有效），缺的只是纯客户端生命感。模型 107 参数含 `ParamBreath/ParamEyeLOpen/ParamEyeROpen/ParamEyeBallX/Y` 全齐；Turn Phase 词条明确 idle 相位归客户端所有——领域许可证现成。

## What Changes

客户端 `live2d_view.dart` 内嵌运行时加四件套：
- 自动眨眼：`ParamEyeLOpen/ROpen` 随机 2-6s 间隔快速闭合（与口型循环同风格的轻量定时器）。
- 呼吸：`ParamBreath` 正弦循环（周期 ~4s，幅度温和）。
- idle 轮播：每 8-15s 随机播 idle 组变体之一（替换「永远第一个」），播放中不叠加。
- 视线游移：`ParamEyeBallX/Y` 低幅缓慢漂移，每 5-10s 换目标点（不覆盖说话/听相位的参数——仅 idle 时驱动）。

不与安静档联动（眨眼呼吸是「在场」不是「打扰」，quiet 管发言）；零协议改动；不立 ADR。

## User Stories

1. As a 用户, I want 不说话时球球也在眨眼呼吸看球, so that 陪伴的在场感是真的。

## Non-goals

- 事件联动表现（backchannel 已覆盖）；视线跟随真实比赛焦点（远期）；服务端任何改动。

## Success Criteria

- flutter test：四件套定时器/状态机单测（注入假时钟）；golden 不漂移（idle 层不影响指令路径）；真人浏览器验收一次。

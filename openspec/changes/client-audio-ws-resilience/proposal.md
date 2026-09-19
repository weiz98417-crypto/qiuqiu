# Client Audio & WS Resilience: 音频源泄漏与半开连接

## Why

两个长会话正确性问题：audio_player_native 每次播放都新建 AudioSource 且从不释放——长陪看内存持续上涨；websocket_service 的旧连接 sink.close() 在 try 之外（残坏连接会让重连流程死掉、状态机停发），且 send() 失败不触发重连——socket 半开时 UI 停留在「已连接」直到 25 秒 ping 吞错。

## What Changes

- audio_player_native：跟踪当前 AudioSource；playEncoded 新播放前、pause、dispose 时释放上一来源；来源生命周期抽为可注入的小单元以便单测。
- websocket_service：close 移入 try 并吞错；send 失败（返回 false）触发 _scheduleReconnect，让半开连接进入可见的重连状态。
- 不改协议、不改后端。

## User Stories

1. As a 用户, I want 连续陪看一小时内存不持续上涨, so that 手机不发热不掉帧。
2. As a 用户, I want 网络半开时界面如实显示重连中, so that 我知道球球暂时听不见我。
3. As a 维护者, I want 音频来源生命周期有单测, so that 释放时机回归有网。

## Non-goals

- Web 播放路径（audio_player_web 已正确处理自动播放拦截，不动）。
- 重连退避加抖动（可选后续）。
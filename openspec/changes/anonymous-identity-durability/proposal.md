# Anonymous Identity Durability: 匿名身份的持久保管

## Why

匿名身份（deviceId → usr_ 映射，后端 anonymous_device_identities 表已支持归还）是画像、关系阶段与全部记忆的挂载点，但承载它的 UUID 只存在 SharedPreferences：清应用数据、部分清除、安全存储缺位等场景下 UUID 丢失 → 客户端生成新 UUID → 新身份，球球把用户忘干净。401 重建会话路径是否始终复用同一 deviceId 也需核实。设备 ID 是匿名身份的唯一凭据，本变更把它从「随手放」改为「妥善保管」。

## What Changes

- deviceId 存储迁 flutter_secure_storage（与令牌同库，含内存回退）；首次读取时若无 secure 值则迁移既有 SharedPreferences 值（迁移语义：绝不能因换存储键生成新 UUID）。
- secure storage 完全不可用时的回退路径保持现状语义（SharedPreferences 兜底）。
- 核实并修正 401 重建会话路径：重建必须复用已存 deviceId（后端已支持归还，同一 deviceId 返回同一 usr_）。
- 后端隐私删除状态（409 ErrIdentityUnavailable）下客户端如实重置本地身份，不假装找回。
- Android 卸载/清数据仍会丢（无账号边界，ADR-0001 不破）——如实记录为已知边界。

## User Stories

1. As a 用户, I want 清除登录凭据后球球还记得我, so that 一次认证故障不会清空我的画像和关系。
2. As a 用户, I want 应用更新后身份不变, so that 升级不是失忆。
3. As a 用户, I want 我行使隐私删除权后身份如实重置, so that 删除就是删除，不会被旧身份污染。
4. As a 维护者, I want deviceId 的存取收敛在一个函数后, so that 换存储不产生新身份。
5. As a 维护者, I want 迁移语义有测试锁定, so that 存储切换永远不生成第二个 UUID。

## Non-goals

- 账号体系 / 卸载重装找回（ADR-0001 无账号边界；届时另立决策）。
- 多设备身份合并（deviceId 天然单设备）。
- 后端改动（anonymous_device_identities 归还机制已存在，migration 026）。
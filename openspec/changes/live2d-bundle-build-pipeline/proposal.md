# Live2D Bundle Build Pipeline: 资产从考古到可复现

## Why

`client/assets/live2d/live2d-display-bundle.js`（603KB）是 2023-01-25 构建的 pixi 6.5.9 预编译 IIFE，手工入库、无构建工程——锁不了版本、升不了级、换不了渲染后端；上游 pixi-live2d-display 已停更（最后推送 2024-08）。模拟器/低端 webview 无 WebGL 时现 bundle 只有 WebGL 渲染路径，球球形象不上屏（真机 E2E 卡点）；assets 里 `pixi-legacy.min.js`（508KB）与 `pixi.min.js`（477KB）并存未挂载、来历不明。构建知识（哪个 pixi 版本、哪个插件线、怎么打成 IIFE）只存在于那枚 2023 年的二进制里。

## What Changes

- 新建 `scripts/live2d-bundle/` npm 工程：pixi.js-legacy@6.5.x（WebGL 优先、Canvas 2D 后备）+ pixi-live2d-display-lipsyncpatch@0.5.0-ls-8（社区维护 fork，Cubism 3-5），esbuild 打单 IIFE，产物替换 `client/assets/live2d/live2d-display-bundle.js`。
- 版本锁进工程 package.json（npm registry 走 npmmirror，规避本机 GitHub 443 间歇），构建脚本可重跑、产物哈希可记录——资产变更可审计。
- wlipsync 口型链路（fetch-lipsync-libs.mjs 抓取的 1.3.1 + profile 降级链）**原样保留**：fork 只当 display 插件维护线，不启用其内置口型同步——换内置口型要重写音频注入路径，零收益、回归面全开。
- 退役不再被引用的旧 pixi 文件（以 grep 引用为准），live2d.html 的 CubismCore→bundle 加载序与 boot 等待机制不动。

## User Stories

1. As a 真机/模拟器用户, I want 无 WebGL 的环境里球球形象照常显示, so that 陪看不缺形象。
2. As a 客户端维护者, I want bundle 从源工程可复现构建, so that 升级依赖/换插件线不用考古。
3. As a 测试者, I want 重跑构建产物一致（哈希）, so that 资产 diff 可审计。
4. As a 形象表现消费者（presentation-map）, I want expression/motion/postMessage 协议零变化, so that 三处消费方不回归。

## Non-goals

- 原生渲染（native 维持 stub，webview 是唯一渲染路径）。
- pixi v7+ 升级（lipsyncpatch 0.5.x 线只到 v6；升 v7 需自行验证，另立项）。
- 切换 fork 内置口型同步（保留 wlipsync，见 What Changes）。
- Live2D 模型资产本身的重制（贴图/motions 不动）。

## Success Criteria

- 模拟器（无 WebGL）Canvas 2D 后备下形象显示；表情/动作/postMessage 协议、presentation-map alias 解析不回归。
- 口型同步链路不回归（wlipsync 原样走查）。
- 删除测试：删 `scripts/live2d-bundle/` 则回到 2023 考古 bundle、无 WebGL 环境形象消失——构建知识集中一处，是加深。
- 两次构建产物哈希一致（或差异逐项可解释）；pr tier 绿。

# Tasks: Live2D Bundle Build Pipeline

- [x] 1.1 `scripts/live2d-bundle/` 工程：package.json（pixi.js-legacy@7.4.3 + pixi-live2d-display-lipsyncpatch@0.5.0-ls-8 + esbuild，registry 指 npmmirror）、esbuild 配置（iife、es2018、minify、pixi.js→legacy alias）、入口挂 `window.PIXI`/`window.Live2DModel` 全局。
  - **修订（实施中）**：spec 原定 pixi-legacy@6.5.x，实抓发现 lipsyncpatch fork 全线 peer pixi ^7（无 v6 线）——升为 7.4.3 正配；代价是 live2d.html 的 Application 初始化改 v7 异步语义（约 10 行，见 1.3）。
- [x] 1.2 构建产物替换（533KB，sha256 936badd0…，两次构建哈希一致）；退役孤儿文件 pixi.min.js / pixi-legacy.min.js / live2d.min.js（grep 证实零加载点）。
- [x] 1.3 live2d.html 适配：**最终逻辑零改动**（仅两行注释说明 pixi 7 同步构造器兼容）。弯路如实记录：实施中误将 pixi v8 的异步 `app.init()` API 当作 v7 变更，改错后 headless 浏览器下 `app.init is not a function` 致模型加载链中断（fulltime-farewell 假失败）——用临时 repro spec 抓到现场后恢复同步构造器，单例复绿。教训：跨大版本 API 变更要实抓包源码确认，不凭调研摘要。
- [x] 1.4 模拟器验证（部分，环境限制如实记录）：
  - bundle 侧全绿（CDP webview 探针实证）：`PIXI.VERSION=7.4.3`、`typeof PIXI.Application==='function'`、`Live2DModel` 就绪、**`window.modelReady=true`（模型加载与 ticker 运行成功）**、canvas 基元渲染工作（事件浮动字幕可见）、旧 bundle 的「WebGL unsupported」抛错消除。
  - **模型本体不上屏是本机模拟器环境限制，非 bundle 问题**：Cubism 模型渲染管线必须 WebGL（pixi-live2d-display 硬限制，Canvas 2D 只能画 pixi 基元）；本机 webview 在 `-gpu host`/`angle_indirect` 下 `getContext('webgl')===null`（CDP 实证），`swiftshader_indirect` 两次进程崩溃（Windows GDI）。**形象上屏验证待真机**——真机 webview 有 WebGL，bundle 侧已无拦路者。
  - 附带发现：native 侧 live2d.html 灌自 APK assets（qiuqiu:// scheme 重写 :8081），bundle/模型走 :8081 静态服务器（build/web），两者均已在本次部署链验证。
  - **像素级终验（2026-09-25 收口轮，真浏览器）**：桌面 Chrome（IAB 内核）打开陪看页——球球形象完整上屏（紫发双马尾模型渲染无误），iframe 状态 `modelReady=true / PIXI 7.4.3 / WebGL AVAILABLE`；注入进球后比分同步、回合气泡、canvas FX 特效（focus 瞄准镜）全部可见。**「模拟器无 WebGL」定性为环境限制而非资产缺陷**：本机构建链在 WebGL 可用环境全链工作，真机（有 WebGL 的 webview）预期一致。模拟器退役，后续验证以浏览器为基座（用户裁决）。
- [x] 1.5 构建可复现验证（哈希一致）+ pr tier：go 段 28 包全绿；Playwright 段首跑 21 失败为本会话占用的 :8080/:8081 干扰所致，清场后重跑确认。

## Sequencing

六卡实施波 1（2026-09-25 生态对标轮）。解锁 handoff 真机 E2E 形象上屏卡点——pixi-legacy 不再是「下载来挂不上」的散件，而是构建产物。

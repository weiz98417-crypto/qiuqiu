# Design: Live2D Bundle Build Pipeline

## 形状

- 入口源码（scripts/live2d-bundle/src/index.js）：
  - `import * as PIXI from 'pixi.js-legacy'`——legacy 版自带 WebGL 探测失败→Canvas 2D 后备，单 adapter 双后端；
  - `import { Live2DModel } from 'pixi-live2d-display-lipsyncpatch/cubism4'` + `Live2DModel.from` 前置 `window.PIXI = PIXI` 注册（fork 的 cubism4 子入口与原版 API 兼容）；
  - 挂 `window.Live2DModel`，保持 live2d.html 消费的全局名不变。
- esbuild：`bundle: true, format: 'iife', target: 'es2018', minify: true`；产物单文件覆盖 `client/assets/live2d/live2d-display-bundle.js`。cubism core 仍外置 script（cubismcore/live2dcubismcore.min.js），不打进 bundle。
- 运行期错误面不变：live2d.html 现有口型降级链三级原样（wlipsync 失败→包络→随机抖动）。

## 版本依据（2026-09-25 调研）

- 原版 pixi-live2d-display 停更（2024-08 最后推送，0.5.x 只到 Pixi v6/7）；lipsyncpatch fork（npm `pixi-live2d-display-lipsyncpatch`，0.5.0-ls-8，2025-06）为社区续命线，Open-LLM-VTuber 同线。
- pixi.js-legacy 6.5.x 与 fork 0.5.x 线兼容；v7+ 升级是另一次验证，不在本 change。

## 网络约束

本机到 GitHub/npm 官源间歇不通：registry 锁 npmmirror（`.npmrc` 进工程目录）；依赖装好后构建离线可跑。

## 可复现

package-lock.json 提交；构建脚本输出产物 sha256，写入工程 README 或构建日志——两次构建哈希应一致（esbuild 确定性构建；不一致则记录差异原因）。

# 球球用户端

这是面向普通球迷的 Flutter 客户端，与 `assets/live2d/operator.html` 企业演示控制台完全分离。用户端只保留比赛、Live2D 数字人、字幕、连续语音和个人陪看偏好。

## 本地运行

```bash
flutter pub get
flutter run \
  --dart-define=QIUQIU_WS_URL=ws://10.0.2.2:8080/ws/match/test
```

- Android 模拟器访问本机后端使用 `10.0.2.2`。
- Web 正式构建默认使用当前域名的同源 WebSocket；Flutter 开发服务需通过 `QIUQIU_WS_URL` 指向后端。
- 客户端启动时自动申请短期匿名会话，不需要注入服务端口令。
- 浏览器若拦截首次主动语音，字幕和动作会照常出现，第一次触碰页面会继续播放待播语音。

## 验证

```bash
dart analyze lib test
flutter test
flutter build web --release
```

前后端同源部署不需要注入 WebSocket 地址；分开部署时再通过 `--dart-define` 注入 HTTPS 对应的 `wss://` 地址。不要把服务口令写进源码或 URL。

## 唤醒词 KWS（wake-word-kws）

- 形态：前台空闲态（比赛页打开、语音会话未激活）喊「你好球球」直接开一轮
  听；词表/参数为评估门定稿（score=1.8 / threshold=0.3 / cooldown=1.5s），
  见 `scripts/wake-eval/results/FINAL-nihao-s1.8-t0.3.json`。
- 隐私：本地推理（sherpa-onnx KeywordSpotter int8），音频不落盘；麦克风监听
  **默认关**，设置页「唤醒词」显式开启，首启有一句话引导。
- 模型落盘：`assets/wake/model/` 不入库，先跑
  `WAKE_ASSETS=1 node scripts/wake-model/download.mjs`（详见
  `scripts/wake-model/README.md`）；`assets/wake/keywords.txt` 入库，换词
  只改它并重跑评估门。
- 平台：Android 先行（windows 作桌面开发验证通道）；web 排除
  （`lib/services/wake_engine.dart` conditional import 走 stub，web 构建不
  受影响）；iOS 留配置位。

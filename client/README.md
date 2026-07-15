# 球球用户端

这是面向普通球迷的 Flutter 客户端，与 `assets/live2d/operator.html` 企业演示控制台完全分离。用户端只保留比赛、Live2D 数字人、字幕、连续语音和个人陪看偏好。

## 本地运行

```bash
flutter pub get
flutter run \
  --dart-define=QIUQIU_WS_URL=ws://10.0.2.2:8080/ws/match/test \
  --dart-define=QIUQIU_APP_TOKEN=本地服务口令
```

- Android 模拟器访问本机后端使用 `10.0.2.2`。
- Web 正式构建默认使用当前域名的同源 WebSocket；Flutter 开发服务需通过 `QIUQIU_WS_URL` 指向后端。
- 本地后端未设置 `APP_TOKEN` 时，可以省略 `QIUQIU_APP_TOKEN`。
- 浏览器若拦截首次主动语音，字幕和动作会照常出现，第一次触碰页面会继续播放待播语音。

## 验证

```bash
dart analyze lib test
flutter test
flutter build web --release
```

前后端同源部署不需要注入 WebSocket 地址；分开部署时再通过 `--dart-define` 注入 HTTPS 对应的 `wss://` 地址。不要把服务口令写进源码或 URL。

# Tasks: Ambient Audio Observation

- [ ] 6.1 SenseVoice AED sidecar：docker-compose 服务 + 无状态 HTTP 接口（POST 音频分片→JSON 事件），fake sidecar 供 pr tier。
- [ ] 6.2 旁路接线：观赛会话音频分片旁送 sidecar → 气氛信号事件 → observation store 旁证（最低权重档）。
- [ ] 6.3 宪法断言测试（气氛事件不进事实账本，负例）+ sidecar 摘除场景测试 + 设置面说明行。
- [ ] 6.4 全量门禁绿。

## Sequencing

六卡实施波 6，**第一个被牺牲项**——前置波（尤其波 3 音频链路）延期则本 change 顺延或取消。依赖 voice-duplex 落定后的音频分片路径稳定。

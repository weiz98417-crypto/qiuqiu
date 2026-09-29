# Tasks: ASR 自托管离线评估与决策

- [ ] 9.1 评估资产:用例集四类(球员名中外文混杂/比分数字/口语碎句/直播间噪声);MiMo vs FunASR 2pass 同批对照脚本;四判据实测(partial 首 token / final 准确率 / 热词命中 / CPU 占用)。
- [ ] 9.2 决策记录:过/不过结论+数据落 docs/design/asr-selfhost-eval.md;过门则替换 change 的 proposal 草案随附(降级链/热词接入/会话上限迁移),不过门则如实记录维持 MiMo。

## Sequencing

波E(评估门)。独立于主路施工,可在波C 之后任意时间跑;CPU 环境即可(FunASR Docker 或本机)。

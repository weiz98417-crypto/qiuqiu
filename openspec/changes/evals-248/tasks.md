# Tasks: Evals 248

- [ ] 1.1 基建（2026-09-23 实测补充：client-microphone 欢迎流程用例在**全套顺序执行**下抖动、单跑 6/6 绿——固定等待脆弱类的第二个实例，awaitTurn 改造时一并治理）：固定 waitForTimeout 清零 + awaitTurn 后端状态轮询 helper + 受影响存量 spec 迁移。
- [ ] 1.2 router 真网档 15 例（开关控制）+ submitDueReminders WS 级测试（留尾清偿）。
- [ ] 1.3 随 change 落地带例：I 域 8（login）、E 域 6（knowledge-event-triggers）、D 域节点 4+失球 2（proactive-match-nodes）、B 域 2（live2d-motion-revert 映射回归）、C 域召回例（memory-recall-fusion 后补全）。
- [ ] 1.4 存量域集中策展：A 红线 22 / B 人格余量 16 / D 陪伴余量 / F 多轮 14 / G 鲁棒 14 / H 语音 10（AnySearch 素材→人工校准）。
- [ ] 1.5 judge 离线档脚本 + 首轮基线报告。
- [ ] 1.6 目录聚合计数 248 对表 + 全套资料口径更新（G 落地后一次做）。
- [ ] 1.7 验证：248 全绿 + CI 时长记录。

## Sequencing

架构评审 G 项（~6-8 人日含策展）。1.3 随对应 change 同 PR；1.4 独立推进；1.1/1.2 最先（其他域的新例都吃 awaitTurn 红利）。

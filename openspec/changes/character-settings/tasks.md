# Tasks: Character Settings

- [x] 5.1 relationship.CharacterSettings（Apply + 校验 + 不变式）。
- [x] 5.2 WS set_character 消息（即改即 ack）+ HTTP /api/me/character + Ledger 记账。
- [x] 5.3 运营台只读段 + 一致性/记账单测。
- [x] 5.4 验证：全量 go test + eval 绿。

## Sequencing

第二波第 4 个；不依赖其他 change，放在订阅后因其 WS 模式可复用 4.4 的测试基建。

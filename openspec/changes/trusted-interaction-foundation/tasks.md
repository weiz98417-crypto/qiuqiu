# Trusted Interaction Foundation Tasks

## Contract

- [x] 冻结用户会话、运营令牌和来源身份的边界
- [x] 冻结事实状态和公开事实视图字段
- [x] 冻结隐私保留、删除和 tombstone 元数据
- [x] 冻结幂等记录和 Outbox 字段

## Migration

- [ ] 执行 `013_trusted_interaction_foundation.sql` 的 PostgreSQL 集成测试
- [ ] 检查已有数据库升级和重复启动行为
- [ ] 记录生产备份与回滚窗口

## Follow-up implementation

- [ ] 实现会话签发、刷新、撤销和统一身份中间件
- [ ] 将用户 WebSocket 身份绑定到令牌 `sub`
- [ ] 将比赛读路径切换到 `PublicFactView`
- [ ] 实现事实确认、撤销、冲突调和和版本查询
- [ ] 实现用户导出、删除、清理任务和 tombstone 检查
- [ ] 实现导播幂等处理和 Outbox 发布器
- [ ] 补齐两用户隔离、事实状态转换、删除回写防护和并发幂等测试

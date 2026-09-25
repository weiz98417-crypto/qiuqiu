-- 画像维护（openspec/changes/portrait-maintenance）阶段一收口：同槽至多
-- 一条开放行。051 把主键换成 id 后，同槽多行成为时间化语义的前提，但也
-- 打开了「判定器误把 UPDATE/DELETE 判成 ADD、同槽并出两条开放行」的落地
-- 口子。本迁移加部分唯一索引兜底：同槽至多一条 valid_to IS NULL 的开放行
-- （含墓碑行——已遗忘槽位不因新增行复活）。
--
-- 落地路径与此索引天然兼容：Put 与 UPDATE/DELETE 都是「先封口后插入」，
-- 封口步先行释放槽位；冲突操作集的 ADD 另有落地前检查（ApplyPortraitOp
-- 遇同槽开放行显式报 ErrSlotOccupied + 审计 skipped），索引只是数据库侧
-- 的最后防线。存量数据经 051 迁移后每槽至多一行，全部 valid_to IS NULL，
-- 建索引不会冲突。

CREATE UNIQUE INDEX IF NOT EXISTS idx_portrait_overlays_open_slot
  ON portrait_overlays(user_id, topic, sub_topic)
  WHERE valid_to IS NULL;

-- 画像维护（openspec/changes/portrait-maintenance）阶段一：时间窗与行身份。
--
-- portrait_overlays 是「下一回合所见」的权威层（041）。此前每槽一行、只靠
-- deleted 墓碑，新旧主张冲突无消解机制，被顶替的条目也无从回放。本迁移把
-- 表变成时间化账本：
--   id          行身份，冲突操作集的 TargetID 指它；
--   valid_from  生效起点（NULL=全域有效，存量数据迁移后全 NULL、语义不变）；
--   valid_to    失效终点（NULL=仍然有效；封口=置 now，行保留、永不物理删）。
--
-- 主键从 (user_id, topic, sub_topic) 换成 id：UPDATE 的确定性落地要写
-- 「旧条目封口 + 新条目生效」两行，同槽多行是历史语义的前提。当前视图=
-- 窗口内行，由读取路径（CurrentPortrait）过滤；全历史读法（PortraitHistory）
-- 不过滤 valid_to/deleted，供可解释性与调试。槽定位查询仍以 user_id 前缀
-- 走 idx_portrait_overlays_user。全账号隐私删除（DeleteUserOverlays）是
-- 唯一的物理删除例外，纪律不变。

ALTER TABLE portrait_overlays
  ADD COLUMN id BIGSERIAL,
  ADD COLUMN valid_from TIMESTAMPTZ,
  ADD COLUMN valid_to TIMESTAMPTZ;

ALTER TABLE portrait_overlays DROP CONSTRAINT portrait_overlays_pkey;
ALTER TABLE portrait_overlays ADD PRIMARY KEY (id);

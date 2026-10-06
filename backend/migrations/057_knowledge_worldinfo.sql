-- 知识域检索后处理参数学（openspec/changes/knowledge-worldinfo，只抄
-- SillyTavern World Info 算法思想、Go 自写）：五字段全部带默认值=现状
-- 行为（管道是可选层，存量 121 条零感知）——
--   priority        预算裁剪序（越大越先占预算，0=不参与重排）；
--   inclusion_group 互斥组（同组多条命中只活一个，''=不互斥）；
--   sticky_turns    命中后连续话题保位 N 轮（0=无状态）；
--   cooldown_turns  命中后冷却 N 轮（0=无状态）；
--   probability     触发概率 ∈(0,1]（1=必中；0 在应用层归一化为 1）。
ALTER TABLE knowledge_entries ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 0;
ALTER TABLE knowledge_entries ADD COLUMN IF NOT EXISTS inclusion_group TEXT NOT NULL DEFAULT '';
ALTER TABLE knowledge_entries ADD COLUMN IF NOT EXISTS sticky_turns INTEGER NOT NULL DEFAULT 0;
ALTER TABLE knowledge_entries ADD COLUMN IF NOT EXISTS cooldown_turns INTEGER NOT NULL DEFAULT 0;
ALTER TABLE knowledge_entries ADD COLUMN IF NOT EXISTS probability DOUBLE PRECISION NOT NULL DEFAULT 1;

-- 知识条目 DB 化（openspec/changes/knowledge-curation-console，ADR-0017 修订）：
-- repo YAML（backend/knowledge/rules、players）降级为 seed，运行时读 DB。
-- 自然键 = 条目 id（repo YAML id，策展身份稳定）；answer 原文即答案锚、
-- topics 关键词、source/confidence/effective_at 照 ADR-0017 字段不变；
-- triggers/quote 承载判罚事件附句条目（ADR-0017 2026-09-23 修订）。
-- created_by/created_at/updated_at 是策展审计元数据；operator 归属
-- 同时落 operator_audit（executeOperatorWrite 写路径纪律）。
CREATE TABLE IF NOT EXISTS knowledge_entries (
    id TEXT PRIMARY KEY,
    topics JSONB NOT NULL,
    answer TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT '',
    confidence DOUBLE PRECISION NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    triggers JSONB NOT NULL DEFAULT '[]'::jsonb,
    quote TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

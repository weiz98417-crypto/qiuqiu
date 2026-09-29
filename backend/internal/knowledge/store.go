package knowledge

// 知识条目存储接缝（openspec/changes/knowledge-curation-console，ADR-0017
// 修订）：条目从 repo YAML 升级为 DB 存储，YAML 降级为 seed。Store 是
// ADR-0006 惯例的「每个本地存储至少两套实现」——内存实现（测试/裸跑）与
// Postgres 实现（postgres.go）；策展写入走 Put（保存即生效），运行时读由
// Library 以 Store 快照承载（Reload 换血，检索双路语义不变）。

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrNotFound 条目不存在（Get 落空）。
var ErrNotFound = errors.New("knowledge entry not found")

// Record 是条目加策展审计元数据：Entry 本体（ADR-0017 字段）之外记录
// 谁建的、何时建/改——operator 归属同时落 operator_audit（写路径纪律），
// 这里的 created_by 只是条目面的静态留痕（seed 导入记 "seed"）。
type Record struct {
	Entry
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Store 是策展面的持久化接缝。List/Get 供运营台读与 Library 装载；Put 是
// 保存即生效的 upsert（存在则整条更新，created_by/created_at 保留首建值）。
type Store interface {
	List(ctx context.Context) ([]Record, error)
	Get(ctx context.Context, id string) (Record, error)
	Put(ctx context.Context, entry Entry, operator string) (Record, error)
}

// preparePut 归一化并校验条目，两套实现共用一套口径：id/answer/topics 必填、
// confidence ∈ [0,1]、带 triggers 必带 quote（织写锚，ADR-0017 2026-09-23
// 修订）、effective_at 必填（生效窗口是第二事实域的组成部分）。
func preparePut(entry Entry) (Entry, error) {
	entry.ID = strings.TrimSpace(entry.ID)
	entry.Answer = strings.TrimSpace(entry.Answer)
	entry.Source = strings.TrimSpace(entry.Source)
	entry.Quote = strings.TrimSpace(entry.Quote)
	if entry.ID == "" {
		return Entry{}, errors.New("knowledge entry id is required")
	}
	if entry.Answer == "" {
		return Entry{}, errors.New("knowledge entry answer is required")
	}
	if len(normalizeWords(entry.Topics)) == 0 {
		return Entry{}, errors.New("knowledge entry topics are required")
	}
	entry.Topics = normalizeWords(entry.Topics)
	entry.Triggers = normalizeWords(entry.Triggers)
	if len(entry.Triggers) > 0 && entry.Quote == "" {
		return Entry{}, errors.New("knowledge entry has triggers but no quote (weave anchor required)")
	}
	if entry.Confidence < 0 || entry.Confidence > 1 {
		return Entry{}, errors.New("knowledge entry confidence must be within [0,1]")
	}
	if entry.EffectiveAt.IsZero() {
		return Entry{}, errors.New("knowledge entry effective_at is required")
	}
	entry.EffectiveAt = entry.EffectiveAt.UTC()
	return entry, nil
}

func normalizeWords(words []string) []string {
	normalized := make([]string, 0, len(words))
	for _, word := range words {
		word = strings.TrimSpace(word)
		if word != "" {
			normalized = append(normalized, word)
		}
	}
	return normalized
}

// MemoryStore 是进程内 Store（测试与无库降级）。
type MemoryStore struct {
	mu      sync.Mutex
	entries map[string]Record
	now     func() time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{entries: make(map[string]Record), now: time.Now}
}

func (m *MemoryStore) List(_ context.Context) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	records := make([]Record, 0, len(m.entries))
	for _, record := range m.entries {
		records = append(records, record)
	}
	sortRecords(records)
	return records, nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.entries[strings.TrimSpace(id)]
	if !ok {
		return Record{}, ErrNotFound
	}
	return record, nil
}

func (m *MemoryStore) Put(_ context.Context, entry Entry, operator string) (Record, error) {
	entry, err := preparePut(entry)
	if err != nil {
		return Record{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	record := Record{Entry: entry, CreatedBy: strings.TrimSpace(operator)}
	if existing, ok := m.entries[entry.ID]; ok {
		record.CreatedBy = existing.CreatedBy
		record.CreatedAt = existing.CreatedAt
	} else {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	m.entries[entry.ID] = record
	return record, nil
}

var _ Store = (*MemoryStore)(nil)

func sortRecords(records []Record) {
	// 按 id 排序：列表确定性（运营台翻页稳定），与检索置信度排序无关。
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
}

// LastTransferWindowClose 返回 now 之前（含）最近的转会窗关闭点：每年
// 7 月 1 日与 1 月 1 日（UTC）。ADR-0017 转会窗复查制度的机器面——条目
// 生效窗口早于最近一次窗闭即「待复查」。
func LastTransferWindowClose(now time.Time) time.Time {
	now = now.UTC()
	july := time.Date(now.Year(), time.July, 1, 0, 0, 0, 0, time.UTC)
	january := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
	if !july.After(now) {
		return july
	}
	return january
}

// DueReview 判定条目是否待复查：生效时间早于最近一次转会窗关闭 = 该条目
// 未经历最近一轮窗后复查（球员档案快变字段尤其如此）。
func DueReview(now, effectiveAt time.Time) bool {
	if effectiveAt.IsZero() {
		return false
	}
	return effectiveAt.Before(LastTransferWindowClose(now))
}

// EntryStatus 是生效窗口二态：active = 已生效（effective_at <= now），
// pending = 待生效（effective_at > now）。
func EntryStatus(now, effectiveAt time.Time) string {
	if effectiveAt.After(now) {
		return "pending"
	}
	return "active"
}

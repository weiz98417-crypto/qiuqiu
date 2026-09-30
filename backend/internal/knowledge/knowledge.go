// Package knowledge 是知识域（第二事实域，ADR-0017）：repo 策展条目的
// 加载与检索。条目原文即答案锚——本包只做「检索命中」，绝不生成事实；
// embedding 缺席时自动退关键词单路（与语义记忆同一降级哲学）。
package knowledge

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MinConfidence 低于它的条目不出答案——「不知道」好过「不太对」。
const MinConfidence = 0.6

// Embedder 是检索侧嵌入能力接缝（internal/embedding.Client 结构性满足）。
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type Entry struct {
	ID          string    `yaml:"id" json:"id"`
	Topics      []string  `yaml:"topics" json:"topics"`
	Answer      string    `yaml:"answer" json:"answer"`
	Source      string    `yaml:"source" json:"source"`
	Confidence  float64   `yaml:"confidence" json:"confidence"`
	EffectiveAt time.Time `yaml:"effective_at" json:"effective_at,omitempty"`
	// Triggers 是事件触发通道（knowledge-event-triggers）：命中即把条目
	// 作为判罚时刻的知识附句素材。仅判罚类事件类型。
	Triggers []string `yaml:"triggers" json:"triggers,omitempty"`
	// Quote 是织写锚：策展短引文，realizer 织写时必须引号原样携带（运行
	// 时 contains 守卫），失败降级确定性附句。带 triggers 的条目必填。
	Quote string `yaml:"quote" json:"quote,omitempty"`
}

type Library struct {
	// store 是策展存储接缝（ADR-0017 修订：DB 为事实源）：非 nil 时 Reload
	// 从它换血条目快照；文件直读模式（无库降级）为 nil。
	store Store
	// entriesMu 守护 entries/triggerIdx 的换血（Reload 整体替换，读取方
	// 取快照后锁外迭代——切片只换不改，快照永远自洽）。
	entriesMu sync.RWMutex
	entries   []Entry
	embedder  Embedder

	mu         sync.Mutex
	topicVecs  map[int][]float32 // 惰性嵌条目主题（topics 以空格相连）
	triggerIdx map[string][]int  // 事件类型 → 条目下标（confidence 降序）
}

// Load 递归读取目录下全部 *.yaml 条目（子目录如 rules/、players/ 仅作
// 组织用途）；embedder 可为 nil（纯关键词检索）。
func Load(dir string, embedder Embedder) (*Library, error) {
	library := &Library{embedder: embedder, topicVecs: map[int][]float32{}}
	err := filepath.WalkDir(dir, func(path string, item os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("knowledge read %s: %w", path, err)
		}
		entry, err := parseEntryFile(path, raw)
		if err != nil {
			return err
		}
		library.entries = append(library.entries, entry)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge dir: %w", err)
	}
	library.buildTriggerIndex()
	return library, nil
}

// NewStoreLibrary 从策展存储装载条目快照（DB 为事实源的运行时读）。
func NewStoreLibrary(ctx context.Context, store Store, embedder Embedder) (*Library, error) {
	if store == nil {
		return nil, fmt.Errorf("knowledge library: nil store")
	}
	library := &Library{store: store, embedder: embedder, topicVecs: map[int][]float32{}}
	if err := library.Reload(ctx); err != nil {
		return nil, err
	}
	return library, nil
}

// Reload 从 store 重取条目并整体换血：运营台保存后调用，编辑即刻进入
// 运行时检索（关键词/向量双路与触发索引全部按新快照重建，语义不变）。
func (l *Library) Reload(ctx context.Context) error {
	if l == nil || l.store == nil {
		return fmt.Errorf("knowledge library: reload requires a store-backed library")
	}
	records, err := l.store.List(ctx)
	if err != nil {
		return fmt.Errorf("knowledge reload: %w", err)
	}
	entries := make([]Entry, len(records))
	for i, record := range records {
		entries[i] = record.Entry
	}
	l.entriesMu.Lock()
	l.entries = entries
	l.buildTriggerIndex()
	l.entriesMu.Unlock()
	// 换血后旧主题向量全部作废（下标与内容都可能变了），缓存清零重嵌。
	l.mu.Lock()
	l.topicVecs = map[int][]float32{}
	l.mu.Unlock()
	return nil
}

// Put 经 store 落一条策展编辑并立即 Reload——「保存即生效」的库内收口。
func (l *Library) Put(ctx context.Context, entry Entry, operator string) (Record, error) {
	if l == nil || l.store == nil {
		return Record{}, fmt.Errorf("knowledge library: put requires a store-backed library")
	}
	record, err := l.store.Put(ctx, entry, operator)
	if err != nil {
		return Record{}, err
	}
	if err := l.Reload(ctx); err != nil {
		return record, err
	}
	return record, nil
}

// Store 返回底层策展存储；文件直读模式返回 nil。
func (l *Library) Store() Store {
	if l == nil {
		return nil
	}
	return l.store
}

// snapshot 取当前条目快照（切片头拷贝）：Reload 只整体替换不原地改，
// 快照在锁外迭代永远自洽。
func (l *Library) snapshot() []Entry {
	l.entriesMu.RLock()
	defer l.entriesMu.RUnlock()
	return l.entries
}

// buildTriggerIndex 建事件类型索引，同类型按 confidence 降序——TriggerLookup
// 取最高确信条目。调用方须持有 entriesMu（Load 构造期单线程除外）。
func (l *Library) buildTriggerIndex() {
	l.triggerIdx = map[string][]int{}
	entries := l.entries
	for i, entry := range entries {
		for _, trigger := range entry.Triggers {
			trigger = strings.TrimSpace(trigger)
			if trigger == "" {
				continue
			}
			l.triggerIdx[trigger] = append(l.triggerIdx[trigger], i)
		}
	}
	for trigger, indexes := range l.triggerIdx {
		sort.SliceStable(indexes, func(a, b int) bool {
			return entries[indexes[a]].Confidence > entries[indexes[b]].Confidence
		})
		l.triggerIdx[trigger] = indexes
	}
}

// TriggerLookup 返回该事件类型的知识附句条目（confidence 最高的命中），
// 无命中 ok=false。走与 Search 相同的确信度阈值。nil 库安全（companion
// 未挂知识库时照常降级为无附句）。entries 与 triggerIdx 必须同一次读锁
// 内取——两次加锁之间夹进 Reload 会让新索引下标套在旧快照上（错条目/
// 越界），快照自洽不变量靠单锁维持。
func (l *Library) TriggerLookup(eventType string) (Entry, bool) {
	if l == nil {
		return Entry{}, false
	}
	l.entriesMu.RLock()
	entries := l.entries
	indexes := l.triggerIdx[strings.TrimSpace(eventType)]
	l.entriesMu.RUnlock()
	for _, index := range indexes {
		if entry, ok := l.guard(entries[index]); ok {
			return entry, true
		}
	}
	return Entry{}, false
}

func (l *Library) Size() int {
	if l == nil {
		return 0
	}
	return len(l.snapshot())
}

// Search 返回最匹配的条目：关键词路（双向 contains，命中数计分）优先，
// 向量路补换说法（余弦 ≥ 0.55）；confidence 低于阈值视同无命中。nil 库安全。
func (l *Library) Search(ctx context.Context, query string) (Entry, bool) {
	if l == nil {
		return Entry{}, false
	}
	entries := l.snapshot()
	if len(entries) == 0 {
		return Entry{}, false
	}
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return Entry{}, false
	}

	best := -1
	bestScore := 0
	for i, entry := range entries {
		score := 0
		for _, topic := range entry.Topics {
			topic = strings.ToLower(strings.TrimSpace(topic))
			if topic == "" {
				continue
			}
			if strings.Contains(normalized, topic) || strings.Contains(topic, normalized) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = i
		}
	}
	if best >= 0 && bestScore > 0 {
		return l.guard(entries[best])
	}

	if l.embedder == nil {
		return Entry{}, false
	}
	queryCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	queryVector, err := l.embedder.Embed(queryCtx, normalized)
	if err != nil {
		return Entry{}, false
	}
	bestVec := -1
	bestCos := 0.0
	for i, entry := range entries {
		vector, ok := l.topicVector(i, entry)
		if !ok {
			continue
		}
		if cos := cosine(queryVector, vector); cos > bestCos {
			bestCos = cos
			bestVec = i
		}
	}
	if bestVec >= 0 && bestCos >= 0.55 {
		return l.guard(entries[bestVec])
	}
	return Entry{}, false
}

// guard 应用确信度阈值：低确信条目视同不知道。
func (l *Library) guard(entry Entry) (Entry, bool) {
	if entry.Confidence < MinConfidence {
		return Entry{}, false
	}
	return entry, true
}

func (l *Library) topicVector(index int, entry Entry) ([]float32, bool) {
	l.mu.Lock()
	if vector, ok := l.topicVecs[index]; ok {
		l.mu.Unlock()
		return vector, true
	}
	embedder := l.embedder
	l.mu.Unlock()
	if embedder == nil {
		return nil, false
	}
	// 嵌入在锁外执行：持锁做网络 IO 会串行化并发查询并放大延迟。
	embedCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	text := strings.Join(entry.Topics, " ")
	vector, err := embedder.Embed(embedCtx, text)
	if err != nil {
		return nil, false
	}
	l.mu.Lock()
	l.topicVecs[index] = vector
	l.mu.Unlock()
	return vector, true
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// Open 是 main 的知识库装配口（ADR-0017 修订）：有 DATABASE_URL 走 DB 存储
// ——repo YAML 降级 seed（幂等导入，已存在条目一律跳过）、运行时读 DB；
// 无库维持 repo YAML 直读（裸跑/测试降级，行为与修订前一致）。返回库与
// 库的底层 store（无库时 store 为 nil，运营台编辑面据此降级 501）。
func Open(ctx context.Context, dir, databaseURL string, embedder Embedder, logf func(format string, args ...any)) (*Library, Store, error) {
	if strings.TrimSpace(databaseURL) == "" {
		library, err := Load(dir, embedder)
		if err != nil {
			return nil, nil, err
		}
		return library, nil, nil
	}
	store, err := OpenPostgresStore(ctx, databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("knowledge store: %w", err)
	}
	if strings.TrimSpace(dir) != "" {
		seeded, err := SeedDir(ctx, store, dir)
		if err != nil {
			store.Close()
			return nil, nil, err
		}
		if seeded > 0 && logf != nil {
			logf("knowledge seed: imported %d entries from %s (existing entries untouched)", seeded, dir)
		}
	}
	library, err := NewStoreLibrary(ctx, store, embedder)
	if err != nil {
		store.Close()
		return nil, nil, err
	}
	return library, store, nil
}

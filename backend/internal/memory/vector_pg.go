package memory

// pgvector 召回路（openspec/changes/semantic-memory）：Moment 嵌入的存取。
// contains 打分路原样保留，本包只承担向量路的写入与余弦检索；任何故障由
// Queue 的降级矩阵吸收（向量路弃权 = 现状行为）。

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// VectorMomentStore 是向量路的接缝：PostgresVectorStore 是生产实现，测试
// 用内存实现（queue 向量测试不依赖真库）。
type VectorMomentStore interface {
	Store(ctx context.Context, moment Moment, embedding []float32) error
	Search(ctx context.Context, userID string, embedding []float32, limit int) ([]Recall, error)
	// SearchByContent 是第三条腿（memory-recall-fusion）：内容精确通道，
	// 稀有实体（裁判名、错别字球员名）余弦不可靠时由它兜住。pattern 由
	// 调用方转义（likePattern）。
	SearchByContent(ctx context.Context, userID, pattern string, limit int) ([]Recall, error)
}

type PostgresVectorStore struct {
	pool *pgxpool.Pool
}

func OpenPostgresVectorStore(ctx context.Context, databaseURL string) (*PostgresVectorStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("vector store connect: %w", err)
	}
	return &PostgresVectorStore{pool: pool}, nil
}

func (s *PostgresVectorStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresVectorStore) Store(ctx context.Context, moment Moment, embedding []float32) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("vector store unavailable")
	}
	tag, err := s.pool.Exec(ctx, `
INSERT INTO embedding_moments (user_id, kind, content, importance, occurred_at, embedding)
VALUES ($1, $2, $3, $4, $5, $6)`,
		moment.UserID, string(moment.Kind), moment.Content, moment.Importance, moment.OccurredAt, embedding)
	if err != nil {
		return fmt.Errorf("vector store insert: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("vector store insert affected no rows")
	}
	return nil
}

func (s *PostgresVectorStore) Search(ctx context.Context, userID string, embedding []float32, limit int) ([]Recall, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("vector store unavailable")
	}
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.pool.Query(ctx, `
SELECT content, importance, occurred_at, 1 - (embedding <=> $2) FROM embedding_moments
WHERE user_id = $1
ORDER BY embedding <=> $2
LIMIT $3`, userID, embedding, limit)
	if err != nil {
		return nil, fmt.Errorf("vector store search: %w", err)
	}
	defer rows.Close()
	recalls := make([]Recall, 0, limit)
	for rows.Next() {
		var recall Recall
		if err := rows.Scan(&recall.Content, &recall.Importance, &recall.OccurredAt, &recall.Score); err != nil {
			return nil, fmt.Errorf("vector store scan: %w", err)
		}
		recall.Source = fmt.Sprintf("vector://moment/%s", userID)
		recalls = append(recalls, recall)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return recalls, nil
}

// likeEscape 转义 ILIKE 模式中的通配符与转义符本身。
func likeEscape(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func (s *PostgresVectorStore) SearchByContent(ctx context.Context, userID, pattern string, limit int) ([]Recall, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("vector store unavailable")
	}
	if limit <= 0 {
		limit = 5
	}
	if strings.TrimSpace(pattern) == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
SELECT content, importance, occurred_at FROM embedding_moments
WHERE user_id = $1 AND content ILIKE '%' || $2 || '%' ESCAPE '\'
ORDER BY occurred_at DESC
LIMIT $3`, userID, likeEscape(pattern), limit)
	if err != nil {
		return nil, fmt.Errorf("vector store content search: %w", err)
	}
	defer rows.Close()
	recalls := make([]Recall, 0, limit)
	for rows.Next() {
		var recall Recall
		if err := rows.Scan(&recall.Content, &recall.Importance, &recall.OccurredAt); err != nil {
			return nil, fmt.Errorf("vector store content scan: %w", err)
		}
		recall.Source = fmt.Sprintf("vector://moment/%s", userID)
		recalls = append(recalls, recall)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return recalls, nil
}

// MemoryVectorStore 是第二个 adapter（测试与无库开发用，带锁——Observe
// 异步写与请求读并发）：线性扫描 + 余弦。
type MemoryVectorStore struct {
	mu      sync.Mutex
	vectors []storedVector
}

type storedVector struct {
	moment    Moment
	embedding []float32
}

func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{}
}

func (s *MemoryVectorStore) Store(_ context.Context, moment Moment, embedding []float32) error {
	if s == nil {
		return fmt.Errorf("vector store unavailable")
	}
	if moment.OccurredAt.IsZero() {
		moment.OccurredAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vectors = append(s.vectors, storedVector{moment: moment, embedding: embedding})
	return nil
}

func (s *MemoryVectorStore) Search(_ context.Context, userID string, embedding []float32, limit int) ([]Recall, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	type scored struct {
		recall Recall
		score  float64
	}
	matches := make([]scored, 0, len(s.vectors))
	for _, stored := range s.vectors {
		if stored.moment.UserID != userID {
			continue
		}
		matches = append(matches, scored{
			recall: Recall{
				Content:    stored.moment.Content,
				Importance: stored.moment.Importance,
				OccurredAt: stored.moment.OccurredAt,
				Source:     fmt.Sprintf("vector://moment/%s", userID),
				Score:      cosine(embedding, stored.embedding),
			},
			score: cosine(embedding, stored.embedding),
		})
	}
	// 简单选择排序取 top-k（规模小，避免引 sort 语义噪音）。
	recalls := make([]Recall, 0, limit)
	for len(recalls) < limit && len(matches) > 0 {
		best := 0
		for i := 1; i < len(matches); i++ {
			if matches[i].score > matches[best].score {
				best = i
			}
		}
		recalls = append(recalls, matches[best].recall)
		matches = append(matches[:best], matches[best+1:]...)
	}
	return recalls, nil
}

func (s *MemoryVectorStore) SearchByContent(_ context.Context, userID, pattern string, limit int) ([]Recall, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	if strings.TrimSpace(pattern) == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lower := strings.ToLower(pattern)
	var matches []Recall
	for _, stored := range s.vectors {
		if stored.moment.UserID != userID {
			continue
		}
		if !strings.Contains(strings.ToLower(stored.moment.Content), lower) {
			continue
		}
		matches = append(matches, Recall{
			Content:    stored.moment.Content,
			Importance: stored.moment.Importance,
			OccurredAt: stored.moment.OccurredAt,
			Source:     fmt.Sprintf("vector://moment/%s", userID),
		})
	}
	// 新的在前（与 Postgres 版 ORDER BY occurred_at DESC 对齐）。
	for i, j := 0, len(matches)-1; i < j; i, j = i+1, j-1 {
		matches[i], matches[j] = matches[j], matches[i]
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
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

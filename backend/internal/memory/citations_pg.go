package memory

// Reflection citation sequences 的 PG 持久化(agent-internals 3.5):
// trackCitation 写穿、takeCitations 消费即删——重启后引用序列审计链不断档
// (此前纯内存,重启静默丢失)。挂 PostgresRecords(与 Reflections 同一 pool),
// Queue 经 CitationStore 窄接口断言使用。

import (
	"context"
)

// AppendCitation 写入一条待反思引用序列(幂等:同 (user, sequence) 重复写
// 无副作用)。
func (r *PostgresRecords) AppendCitation(ctx context.Context, userID string, sequence int64) error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}
	_, err := r.pool.Exec(ctx, `
INSERT INTO memory_citations (user_id, ledger_sequence) VALUES ($1, $2)
ON CONFLICT (user_id, ledger_sequence) DO NOTHING`, userID, sequence)
	if err != nil {
		return err
	}
	return nil
}

// TakeCitations 取走该用户全部待反思引用序列:CTE 删除的同时按序列倒序
// 返回,消费与取值原子。
func (r *PostgresRecords) TakeCitations(ctx context.Context, userID string) ([]int64, error) {
	if r == nil || r.pool == nil {
		return nil, ErrUnavailable
	}
	rows, err := r.pool.Query(ctx, `
WITH taken AS (
  DELETE FROM memory_citations
  WHERE user_id = $1
  RETURNING ledger_sequence
)
SELECT ledger_sequence FROM taken
ORDER BY ledger_sequence DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sequences := []int64{}
	for rows.Next() {
		var sequence int64
		if err := rows.Scan(&sequence); err != nil {
			return nil, err
		}
		sequences = append(sequences, sequence)
	}
	return sequences, rows.Err()
}

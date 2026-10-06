// Package journal 承载球友手记与赛季记忆册（openspec/changes/
// teammate-journal）：reflection 产能的消费面（ADR-0023 第 2 条）。
//
// 宪法线（ proposal Non-goals + CONTEXT 纪律）：
//   - 手记素材只引账本真实行与画像条目，手记永不产生新的比赛事实——
//     生成后经确定性比分校验（正文比分形态必须与账本终场一致，不一致
//     降级确定性底稿），LLM/无 LLM 两条路都零编造；
//   - 手记是阅读面不是聊天素材（不进主动回合话术，避免自我引用循环）；
//   - 隐私生命周期：用户删除 = 物理删（moments 同款）。
package journal

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ErrNotFound 是手记不存在（或已删/不属于该用户）。
var ErrNotFound = errors.New("journal entry not found")

// Entry 是一篇球友手记。比赛事实快照（队名/比分/进球）在生成时从账本
// 终场投影定格——赛后事实不变，快照即账本事实的引用，不是手记自己的事实。
type Entry struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	MatchID   string    `json:"matchId"`
	HomeTeam  string    `json:"homeTeam"`
	AwayTeam  string    `json:"awayTeam"`
	Score     string    `json:"score"`
	Goals     []string  `json:"goals,omitempty"`
	Body      string    `json:"body"`
	Season    string    `json:"season,omitempty"`
	Liked     bool      `json:"liked"`
	Sources   []string  `json:"sources,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Store 是手记持久化面。PG 是生产实现；Memory 是测试与无库部署。
type Store interface {
	// Put 幂等写入（同 user+match 只留一篇——重复 beat 更新正文）。
	Put(ctx context.Context, entry Entry) error
	// List 按创建时间倒序列该用户全部手记。
	List(ctx context.Context, userID string, limit int) ([]Entry, error)
	// Get 取单篇（校验归属）。
	Get(ctx context.Context, userID, entryID string) (Entry, error)
	// SetLiked 点赞/取消（归属校验，物理字段）。
	SetLiked(ctx context.Context, userID, entryID string, liked bool) error
	// Delete 物理删（隐私生命周期，moments 同款）。
	Delete(ctx context.Context, userID, entryID string) error
}

// MemoryStore 是进程内实现（测试与无库部署）。
type MemoryStore struct {
	entries []Entry
}

// Put 幂等写入（同 user+match 更新）。
func (s *MemoryStore) Put(_ context.Context, entry Entry) error {
	for index := range s.entries {
		if s.entries[index].UserID == entry.UserID && s.entries[index].MatchID == entry.MatchID {
			s.entries[index] = entry
			return nil
		}
	}
	s.entries = append(s.entries, entry)
	return nil
}

// List 按创建时间倒序。
func (s *MemoryStore) List(_ context.Context, userID string, limit int) ([]Entry, error) {
	matched := make([]Entry, 0)
	for _, entry := range s.entries {
		if entry.UserID == userID {
			matched = append(matched, entry)
		}
	}
	sort.SliceStable(matched, func(left, right int) bool {
		return matched[left].CreatedAt.After(matched[right].CreatedAt)
	})
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

// Get 取单篇（归属校验）。
func (s *MemoryStore) Get(_ context.Context, userID, entryID string) (Entry, error) {
	for _, entry := range s.entries {
		if entry.UserID == userID && entry.ID == entryID {
			return entry, nil
		}
	}
	return Entry{}, ErrNotFound
}

// SetLiked 点赞/取消。
func (s *MemoryStore) SetLiked(_ context.Context, userID, entryID string, liked bool) error {
	for index := range s.entries {
		if s.entries[index].UserID == userID && s.entries[index].ID == entryID {
			s.entries[index].Liked = liked
			return nil
		}
	}
	return ErrNotFound
}

// Delete 物理删。
func (s *MemoryStore) Delete(_ context.Context, userID, entryID string) error {
	for index := range s.entries {
		if s.entries[index].UserID == userID && s.entries[index].ID == entryID {
			s.entries = append(s.entries[:index], s.entries[index+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// MatchFacts 是生成一篇手记所需的账本终场事实（快照投影）。
type MatchFacts struct {
	MatchID  string
	HomeTeam string
	AwayTeam string
	Score    string
	Goals    []string // 「佩德里 25'」形态的进球清单（账本投影序）
	Season   string
}

// PortraitLine 是生成素材里的画像条目（口味对照用：「你上次说…还真变了」）。
type PortraitLine struct {
	SubTopic string
	Content  string
}

// TextGenerator 是手记织写的 LLM seam（companion realizer 同形态的函数
// 注入；nil/失败 = 确定性底稿，手记面永远有内容且零编造）。
type TextGenerator interface {
	GenerateJournal(ctx context.Context, facts MatchFacts, portrait []PortraitLine) (string, error)
}

var journalScorePattern = regexp.MustCompile(`\d{1,2}\s*[-:：比]\s*\d{1,2}`)

// journalID 是 user+match 的确定性手记 id（一篇一场）。
func journalID(userID, matchID string) string {
	return userID + ":" + matchID
}

// DeterministicDraft 是确定性底稿：纯账本事实句，零 LLM、零编造。
func DeterministicDraft(facts MatchFacts) string {
	var builder strings.Builder
	builder.WriteString("看完这场 " + facts.HomeTeam + " 对 " + facts.AwayTeam + "，比分 " + facts.Score + "。")
	if len(facts.Goals) > 0 {
		builder.WriteString(facts.Goals[0] + " 的进球我还记着呢。")
	} else {
		builder.WriteString("这场球，我还记着呢。")
	}
	return builder.String()
}

// validateBody 是生成的确定性后校验（10.1 evals：零编造比分）：正文里的
// 比分形态必须与账本终场一致——不一致返回 false（调用方降级底稿）。
func validateBody(body string, facts MatchFacts) bool {
	for _, loc := range journalScorePattern.FindAllStringIndex(body, -1) {
		if strings.TrimSpace(body[loc[0]:loc[1]]) != facts.Score {
			return false
		}
	}
	return strings.TrimSpace(body) != ""
}

// Generate 生成并落地一篇手记（赛后 beat 调用，幂等：同场更新）。
// LLM 织写 → 比分校验失败/无 generator/失败 → 确定性底稿。
func Generate(ctx context.Context, store Store, generator TextGenerator, userID string, facts MatchFacts, portrait []PortraitLine) (Entry, error) {
	body := ""
	if generator != nil {
		if drafted, err := generator.GenerateJournal(ctx, facts, portrait); err == nil {
			if validateBody(drafted, facts) {
				body = strings.TrimSpace(drafted)
			}
		}
	}
	sources := make([]string, 0, len(facts.Goals)+1)
	sources = append(sources, "match:"+facts.MatchID)
	for _, goal := range facts.Goals {
		sources = append(sources, "goal:"+goal)
	}
	for _, line := range portrait {
		sources = append(sources, "portrait:"+line.SubTopic)
	}
	if body == "" {
		body = DeterministicDraft(facts)
	}
	entry := Entry{
		ID:        journalID(userID, facts.MatchID),
		UserID:    userID,
		MatchID:   facts.MatchID,
		HomeTeam:  facts.HomeTeam,
		AwayTeam:  facts.AwayTeam,
		Score:     facts.Score,
		Goals:     facts.Goals,
		Body:      body,
		Season:    facts.Season,
		Sources:   sources,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Put(ctx, entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// AlbumPage 是赛季记忆册的一页：一场一条——比赛快照 + 手记摘要 + 共同
// 瞬间（memory-surfacing 的 Moment 投影按场归属）。
type AlbumPage struct {
	MatchID   string   `json:"matchId"`
	Season    string   `json:"season,omitempty"`
	HomeTeam  string   `json:"homeTeam"`
	AwayTeam  string   `json:"awayTeam"`
	Score     string   `json:"score"`
	EntryID   string   `json:"entryId,omitempty"`
	Journal   string   `json:"journal,omitempty"`
	Liked     bool     `json:"liked"`
	Moments   []string `json:"moments,omitempty"`
	CreatedAt string   `json:"createdAt,omitempty"`
}

// MomentSource 是装订时的共同瞬间源（memory.Queue 的 Moment 投影按场
// 过滤——adapter 注入，避免 journal 反向依赖 memory 包）。
type MomentSource interface {
	MomentsForMatch(ctx context.Context, userID, matchID string) []string
}

// BuildAlbum 装订赛季记忆册：手记列表 + 每场的共同瞬间，按 season 过滤
// （空 = 全部赛季），按创建时间倒序。
func BuildAlbum(ctx context.Context, store Store, moments MomentSource, userID, season string) ([]AlbumPage, error) {
	entries, err := store.List(ctx, userID, 0)
	if err != nil {
		return nil, err
	}
	season = strings.TrimSpace(season)
	pages := make([]AlbumPage, 0, len(entries))
	for _, entry := range entries {
		if season != "" && entry.Season != season {
			continue
		}
		page := AlbumPage{
			MatchID:   entry.MatchID,
			Season:    entry.Season,
			HomeTeam:  entry.HomeTeam,
			AwayTeam:  entry.AwayTeam,
			Score:     entry.Score,
			EntryID:   entry.ID,
			Journal:   Summary(entry.Body),
			Liked:     entry.Liked,
			CreatedAt: entry.CreatedAt.UTC().Format(time.RFC3339),
		}
		if moments != nil {
			page.Moments = moments.MomentsForMatch(ctx, userID, entry.MatchID)
		}
		pages = append(pages, page)
	}
	return pages, nil
}

// Summary 是手记摘要：首句（。！?截断，≤60 字）。
func Summary(body string) string {
	body = strings.TrimSpace(body)
	for index, runeValue := range body {
		if runeValue == '。' || runeValue == '！' || runeValue == '?' || runeValue == '？' {
			body = body[:index+len(string(runeValue))]
			break
		}
	}
	runes := []rune(body)
	if len(runes) > 60 {
		return string(runes[:60]) + "…"
	}
	return body
}

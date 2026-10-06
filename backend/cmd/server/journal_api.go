package main

// 球友手记 + 赛季记忆册端点（openspec/changes/teammate-journal）：
// GET    /api/me/journal          手记列表（创建时间倒序）
// PUT    /api/me/journal/{id}/like  点赞/取消（body {liked: bool}）
// DELETE /api/me/journal/{id}     忘掉（物理删，隐私生命周期）
// GET    /api/me/journal/album    赛季记忆册（?season= 过滤，每场一条：
//                                 比分+手记摘要+共同瞬间）
// 鉴权/降级口径与 /api/me/moments 一致（session bearer + ScopeUserRead；
// store 缺位 = 空列表，页面静默隐藏）。

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
	"qiuqiu/internal/journal"
)

type journalEntryResponse struct {
	ID        string   `json:"id"`
	MatchID   string   `json:"matchId"`
	HomeTeam  string   `json:"homeTeam"`
	AwayTeam  string   `json:"awayTeam"`
	Score     string   `json:"score"`
	Goals     []string `json:"goals,omitempty"`
	Body      string   `json:"body"`
	Season    string   `json:"season,omitempty"`
	Liked     bool     `json:"liked"`
	CreatedAt string   `json:"createdAt,omitempty"`
}

type journalListResponse struct {
	Entries []journalEntryResponse `json:"entries"`
}

func handleJournalAPI(manager *auth.Manager, cfg *config.Config, store journal.Store, moments journal.MomentSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !applyCORS(w, r, cfg) {
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		claims, err := authenticateUser(r, manager)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !claims.HasScope(auth.ScopeUserRead) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if store == nil {
			// 无库部署：手记面静默隐藏（空列表），不是错误。
			writeJSON(w, http.StatusOK, journalListResponse{Entries: []journalEntryResponse{}})
			return
		}
		entryID := r.PathValue("entryId")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/album"):
			writeJournalAlbum(w, r, store, moments, claims.Subject)
		case r.Method == http.MethodGet && entryID == "":
			writeJournalList(w, r, store, claims.Subject)
		case r.Method == http.MethodPut && entryID != "" && strings.HasSuffix(r.URL.Path, "/like"):
			likeJournalEntry(w, r, store, claims.Subject, entryID)
		case r.Method == http.MethodDelete && entryID != "":
			deleteJournalEntry(w, r, store, claims.Subject, entryID)
		default:
			w.Header().Set("Allow", "DELETE, GET, PUT, OPTIONS")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func toJournalEntryResponse(entry journal.Entry) journalEntryResponse {
	return journalEntryResponse{
		ID:        entry.ID,
		MatchID:   entry.MatchID,
		HomeTeam:  entry.HomeTeam,
		AwayTeam:  entry.AwayTeam,
		Score:     entry.Score,
		Goals:     entry.Goals,
		Body:      entry.Body,
		Season:    entry.Season,
		Liked:     entry.Liked,
		CreatedAt: entry.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func writeJournalList(w http.ResponseWriter, r *http.Request, store journal.Store, userID string) {
	entries, err := store.List(r.Context(), userID, 100)
	if err != nil {
		http.Error(w, "手记加载失败", http.StatusInternalServerError)
		return
	}
	response := journalListResponse{Entries: make([]journalEntryResponse, 0, len(entries))}
	for _, entry := range entries {
		response.Entries = append(response.Entries, toJournalEntryResponse(entry))
	}
	writeJSON(w, http.StatusOK, response)
}

func writeJournalAlbum(w http.ResponseWriter, r *http.Request, store journal.Store, moments journal.MomentSource, userID string) {
	pages, err := journal.BuildAlbum(r.Context(), store, moments, userID, r.URL.Query().Get("season"))
	if err != nil {
		http.Error(w, "赛季册装订失败", http.StatusInternalServerError)
		return
	}
	if pages == nil {
		pages = []journal.AlbumPage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"album": pages})
}

func likeJournalEntry(w http.ResponseWriter, r *http.Request, store journal.Store, userID, entryID string) {
	var request struct {
		Liked *bool `json:"liked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if request.Liked == nil {
		http.Error(w, "liked is required", http.StatusBadRequest)
		return
	}
	if err := store.SetLiked(r.Context(), userID, entryID, *request.Liked); err != nil {
		if errors.Is(err, journal.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "点赞失败", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "liked": *request.Liked})
}

func deleteJournalEntry(w http.ResponseWriter, r *http.Request, store journal.Store, userID, entryID string) {
	if err := store.Delete(r.Context(), userID, entryID); err != nil {
		if errors.Is(err, journal.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "忘掉手记失败", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

package main

// 共同瞬间端点(memory-surfacing 1.4/1.5):GET /api/me/moments 列举 +
// DELETE /api/me/moments/{momentId} 忘掉(物理删除——隐私生命周期:用户要求
// 忘掉的内容不留存)。鉴权/降级口径与 /api/me/threads 一致(session bearer +
// ScopeUserRead;向量路缺位=空列表,页面静默隐藏)。数据源定稿:Moment 投影
// (embedding_moments,字段含 MatchID/OccurredAt,独立持久化)胜出
// relationship.SharedMoment(StateBundle 形态,无独立列举能力)。

import (
	"net/http"
	"strings"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
	"qiuqiu/internal/memory"
)

type momentEntryResponse struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Content     string  `json:"content"`
	Importance  float64 `json:"importance"`
	OccurredAt  string  `json:"occurredAt,omitempty"`
}

type momentsResponse struct {
	Moments []momentEntryResponse `json:"moments"`
}

func handleMomentsAPI(manager *auth.Manager, cfg *config.Config, memories *memory.Queue) http.HandlerFunc {
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
		switch {
		case r.Method == http.MethodGet && r.PathValue("momentId") == "":
			writeMoments(w, listMoments(w, r, memories, claims.Subject))
		case r.Method == http.MethodDelete && r.PathValue("momentId") != "":
			forgetMoment(w, r, memories, claims.Subject, r.PathValue("momentId"))
		default:
			w.Header().Set("Allow", "DELETE, GET, OPTIONS")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func listMoments(w http.ResponseWriter, r *http.Request, memories *memory.Queue, userID string) []momentEntryResponse {
	moments, err := memories.ListMoments(r.Context(), userID, 50, 0)
	if err != nil {
		// 向量路缺位/降级 = 空列表,不是错误——页面静默隐藏。
		return nil
	}
	entries := make([]momentEntryResponse, 0, len(moments))
	for _, moment := range moments {
		if strings.TrimSpace(moment.Content) == "" {
			continue
		}
		entries = append(entries, momentEntryResponse{
			ID:         moment.ID,
			Kind:       string(moment.Kind),
			Content:    moment.Content,
			Importance: moment.Importance,
			OccurredAt: moment.OccurredAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return entries
}

func forgetMoment(w http.ResponseWriter, r *http.Request, memories *memory.Queue, userID, momentID string) {
	if strings.TrimSpace(momentID) == "" {
		http.Error(w, "momentId is required", http.StatusBadRequest)
		return
	}
	if err := memories.ForgetMoment(r.Context(), userID, momentID); err != nil {
		http.Error(w, "忘掉共同瞬间失败", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeMoments(w http.ResponseWriter, entries []momentEntryResponse) {
	if entries == nil {
		entries = []momentEntryResponse{}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	writeJSON(w, http.StatusOK, momentsResponse{Moments: entries})
}

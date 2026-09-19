package main

// Portrait API — the user-facing half of the C3 portrait (球球懂我 page).
//
// Transport choice: REST on the existing mux under /api/me/portrait, mirroring
// the privacy API exactly (session bearer auth, CORS, JSON). Portrait data is
// account-scoped, not match-scoped, and the operator console is a separate
// surface (DESIGN.md); the WS protocol stays the live watch transport. This
// is the same pattern the repo already uses for user-owned data
// (/api/me/privacy, /api/me/export, /api/me/data).
//
// Reads assemble the Memobase synthesis with the local overlay layer inside
// internal/memory.Queue — the same portrait the realization prompt receives —
// so the page always shows the real stored state, never a decorative copy.
// DELETE writes a local tombstone (migrations/041 portrait_overlays) that the
// seam applies on the very next turn, then forwards best-effort to Memobase.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/privacy"
)

// maxPortraitEditRunes bounds a user edit so a hostile payload cannot inflate
// the bounded portrait block.
const maxPortraitEditRunes = 120

type portraitEntryResponse struct {
	ID         string `json:"id,omitempty"`
	Topic      string `json:"topic"`
	TopicLabel string `json:"topicLabel"`
	SubTopic   string `json:"subTopic"`
	Label      string `json:"label"`
	Content    string `json:"content"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
	Source     string `json:"source,omitempty"`
}

type portraitResponse struct {
	UpdatedAt string                  `json:"updatedAt,omitempty"`
	Entries   []portraitEntryResponse `json:"entries"`
}

func handlePortraitAPI(manager *auth.Manager, cfg *config.Config, memories *memory.Queue) http.HandlerFunc {
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
		userID := claims.Subject
		switch r.Method {
		case http.MethodGet:
			entries, updatedAt := memories.PortraitEntries(r.Context(), userID)
			writePortrait(w, entries, updatedAt)
		case http.MethodPatch:
			handlePortraitEdit(w, r, memories, userID)
		case http.MethodDelete:
			handlePortraitForget(w, r, memories, userID)
		default:
			w.Header().Set("Allow", "DELETE, GET, OPTIONS, PATCH")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func handlePortraitEdit(w http.ResponseWriter, r *http.Request, memories *memory.Queue, userID string) {
	var request struct {
		Topic    string `json:"topic"`
		SubTopic string `json:"subTopic"`
		Content  string `json:"content"`
		EntryID  string `json:"entryId"`
	}
	if err := decodeSessionJSON(w, r, &request); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	topic := strings.TrimSpace(request.Topic)
	subTopic := strings.TrimSpace(request.SubTopic)
	content := strings.TrimSpace(request.Content)
	if topic == "" || subTopic == "" {
		http.Error(w, "topic and subTopic are required", http.StatusBadRequest)
		return
	}
	if len([]rune(content)) == 0 {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if len([]rune(content)) > maxPortraitEditRunes {
		http.Error(w, "content is too long", http.StatusBadRequest)
		return
	}
	// Detached timeout: the edit must survive a client disconnect.
	writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := memories.SetPortraitEntry(writeCtx, userID, topic, subTopic, content, strings.TrimSpace(request.EntryID)); err != nil {
		writePortraitMutationError(w, err, "画像更新失败")
		return
	}
	entries, updatedAt := memories.PortraitEntries(r.Context(), userID)
	writePortrait(w, entries, updatedAt)
}

func handlePortraitForget(w http.ResponseWriter, r *http.Request, memories *memory.Queue, userID string) {
	var request struct {
		Topic    string `json:"topic"`
		SubTopic string `json:"subTopic"`
		EntryID  string `json:"entryId"`
	}
	// An empty body forgets the whole portrait; a topic+subTopic pair forgets one slot.
	if err := decodeSessionJSON(w, r, &request); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	topic := strings.TrimSpace(request.Topic)
	subTopic := strings.TrimSpace(request.SubTopic)
	entryID := strings.TrimSpace(request.EntryID)
	// Detached timeout: the tombstone must survive a client disconnect.
	writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	if topic == "" {
		err = memories.ForgetPortrait(writeCtx, userID)
	} else {
		if subTopic == "" {
			http.Error(w, "subTopic is required to forget one entry", http.StatusBadRequest)
			return
		}
		err = memories.ForgetPortraitEntry(writeCtx, userID, topic, subTopic, entryID)
	}
	if err != nil {
		writePortraitMutationError(w, err, "画像更新失败")
		return
	}
	entries, updatedAt := memories.PortraitEntries(r.Context(), userID)
	writePortrait(w, entries, updatedAt)
}

func writePortrait(w http.ResponseWriter, entries []memory.PortraitEntry, updatedAt time.Time) {
	response := portraitResponse{Entries: make([]portraitEntryResponse, 0, len(entries))}
	if !updatedAt.IsZero() {
		response.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	}
	for _, entry := range entries {
		response.Entries = append(response.Entries, portraitEntryResponse{
			ID:         entry.ID,
			Topic:      entry.Topic,
			TopicLabel: memory.PortraitTopicLabel(entry.Topic),
			SubTopic:   entry.SubTopic,
			Label:      memory.PortraitSubTopicLabel(entry.SubTopic),
			Content:    entry.Content,
			UpdatedAt:  entry.UpdatedAt.UTC().Format(time.RFC3339),
			Source:     entry.Source,
		})
	}
	writeJSON(w, http.StatusOK, response)
}

func writePortraitMutationError(w http.ResponseWriter, err error, defaultMessage string) {
	switch {
	case errors.Is(err, privacy.ErrDataDeleted):
		http.Error(w, "用户数据已删除", http.StatusGone)
	case errors.Is(err, privacy.ErrDeletionInProgress):
		http.Error(w, "用户数据删除进行中", http.StatusConflict)
	case errors.Is(err, memory.ErrUnavailable):
		http.Error(w, "画像存储暂不可用", http.StatusServiceUnavailable)
	case errors.Is(err, memory.ErrNotSupported):
		http.Error(w, "画像存储未启用", http.StatusNotImplemented)
	default:
		http.Error(w, defaultMessage, http.StatusInternalServerError)
	}
}

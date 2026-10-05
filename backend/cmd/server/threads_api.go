package main

// 用户侧未完话题端点(memory-surfacing 1.7):GET /api/me/threads 返回当前
// 用户的开放话题——「上次没聊完的…」条的数据面。鉴权/隐私与 /api/me/portrait
// 同一口径(session bearer + ScopeUserRead,账号 scoped);只读——续聊走既有
// 对话路径,关闭走 recovery 投递,不新开写口。不暴露 match_id 以外的比赛细节
// (内容本身就是用户自己说的话)。

import (
	"log"
	"net/http"
	"strings"
	"time"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
	"qiuqiu/internal/memory"
)

type threadEntryResponse struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type threadsResponse struct {
	Threads []threadEntryResponse `json:"threads"`
}

func handleThreadsAPI(manager *auth.Manager, cfg *config.Config, memories *memory.Queue) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !applyCORS(w, r, cfg) {
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET, OPTIONS")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		threads, err := memories.Threads(r.Context(), claims.Subject)
		if err != nil {
			// 账本缺席/降级 = 空列表,不是错误——客户端话题条静默隐藏;
			// 照「错误只记日志」纪律落一条(非 panic 路径)。
			log.Printf("threads api: list threads for %q: %v", claims.Subject, err)
			writeThreads(w, nil)
			return
		}
		entries := make([]threadEntryResponse, 0, len(threads))
		for _, thread := range threads {
			if thread.State != "open" || strings.TrimSpace(thread.Content) == "" {
				continue
			}
			entries = append(entries, threadEntryResponse{
				ID:        thread.ID,
				Kind:      string(thread.Kind),
				Content:   thread.Content,
				CreatedAt: thread.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
		writeThreads(w, entries)
	}
}

func writeThreads(w http.ResponseWriter, entries []threadEntryResponse) {
	if entries == nil {
		entries = []threadEntryResponse{}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	writeJSON(w, http.StatusOK, threadsResponse{Threads: entries})
}

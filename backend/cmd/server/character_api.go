package main

// /api/me/character（openspec/changes/character-settings）：人格互动规范
// 的 HTTP 入口——GET 读全量、PATCH 改单槽位；会话鉴权与 portrait API 同款。
// WS 侧另有 set_character 消息（即改即 ack），cue 词入口原样保留。

import (
	"encoding/json"
	"net/http"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
	"qiuqiu/internal/relationship"
)

func handleCharacterAPI(manager *auth.Manager, cfg *config.Config, settings *relationship.CharacterSettings) http.HandlerFunc {
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
			values, err := settings.Get(r.Context(), userID)
			if err != nil {
				http.Error(w, "settings unavailable", http.StatusInternalServerError)
				return
			}
			writeSettings(w, values)
		case http.MethodPatch:
			var request struct {
				Field string `json:"field"`
				Value string `json:"value"`
			}
			if err := decodeSessionJSON(w, r, &request); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			updated, err := settings.Set(r.Context(), userID, request.Field, request.Value)
			if err != nil {
				http.Error(w, "invalid setting", http.StatusBadRequest)
				return
			}
			writeSettings(w, updated)
		default:
			w.Header().Set("Allow", "GET, OPTIONS, PATCH")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func writeSettings(w http.ResponseWriter, values map[relationship.CharacterSettingField]string) {
	// 反参考纪律（PRODUCT.md）：对外只说互动偏好，不出现存储字段名以外的
	// 内部概念。
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"initiative":       values[relationship.SettingInitiative],
		"analysisAppetite": values[relationship.SettingAnalysisAppetite],
		"banterLevel":      values[relationship.SettingBanterLevel],
	})
}


package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
)

func handleSessionAPI(manager *auth.Manager, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		if !applyCORS(w, r, cfg) {
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		resource := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/")
		switch {
		case r.Method == http.MethodPost && resource == "anonymous":
			var request struct {
				DeviceID string `json:"deviceId"`
			}
			if err := decodeSessionJSON(w, r, &request); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			session, err := manager.IssueAnonymous(r.Context(), request.DeviceID)
			if err != nil {
				http.Error(w, "session unavailable", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusCreated, sessionResponse(session))
		case r.Method == http.MethodPost && resource == "refresh":
			var request struct {
				RefreshToken string `json:"refreshToken"`
			}
			if err := decodeSessionJSON(w, r, &request); err != nil || strings.TrimSpace(request.RefreshToken) == "" {
				http.Error(w, "refreshToken is required", http.StatusBadRequest)
				return
			}
			session, err := manager.Refresh(r.Context(), request.RefreshToken)
			if err != nil {
				http.Error(w, "invalid session", http.StatusUnauthorized)
				return
			}
			writeJSON(w, http.StatusOK, sessionResponse(session))
		case r.Method == http.MethodPost && resource == "revoke":
			token := auth.BearerToken(r.Header.Get("Authorization"))
			if token != "" {
				if err := manager.RevokeAccess(r.Context(), token); err != nil && !errors.Is(err, auth.ErrRevoked) {
					http.Error(w, "invalid session", http.StatusUnauthorized)
					return
				}
			} else {
				var request struct {
					RefreshToken string `json:"refreshToken"`
				}
				if err := decodeSessionJSON(w, r, &request); err != nil || strings.TrimSpace(request.RefreshToken) == "" {
					http.Error(w, "session token is required", http.StatusBadRequest)
					return
				}
				if err := manager.RevokeRefresh(r.Context(), request.RefreshToken); err != nil && !errors.Is(err, auth.ErrRevoked) {
					http.Error(w, "invalid session", http.StatusUnauthorized)
					return
				}
			}
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}
}

func decodeSessionJSON(w http.ResponseWriter, r *http.Request, destination interface{}) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if err := decoder.Decode(destination); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func sessionResponse(session auth.Session) map[string]interface{} {
	return map[string]interface{}{
		"tokenType":        "Bearer",
		"accessToken":      session.AccessToken,
		"refreshToken":     session.RefreshToken,
		"userId":           session.Claims.Subject,
		"sessionId":        session.Claims.SessionID,
		"expiresAt":        session.Claims.ExpiresAt,
		"refreshExpiresAt": session.RefreshExpiresAt,
		"scopes":           session.Claims.Scopes,
	}
}

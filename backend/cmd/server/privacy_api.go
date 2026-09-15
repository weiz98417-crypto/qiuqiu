package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
	"qiuqiu/internal/privacy"
)

func handlePrivacyAPI(manager *auth.Manager, cfg *config.Config, service *privacy.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !applyCORS(w, r, cfg) {
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		resource := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/me/"), "/")
		if r.Method == http.MethodGet && resource == "privacy" && auth.BearerToken(r.Header.Get("Authorization")) == "" {
			jobID := strings.TrimSpace(r.URL.Query().Get("jobId"))
			if jobID == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			status, err := service.StatusByJob(r.Context(), jobID)
			if err != nil {
				if errors.Is(err, privacy.ErrNotFound) {
					http.NotFound(w, r)
					return
				}
				http.Error(w, "privacy status unavailable", http.StatusInternalServerError)
				return
			}
			status.UserID = ""
			writeJSON(w, http.StatusOK, status)
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
		case r.Method == http.MethodGet && resource == "privacy":
			status, err := service.Status(r.Context(), claims.Subject)
			if err != nil {
				http.Error(w, "privacy status unavailable", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, status)
		case r.Method == http.MethodGet && resource == "export":
			export, err := service.Export(r.Context(), claims.Subject)
			if err != nil {
				writePrivacyError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, export)
		case r.Method == http.MethodDelete && resource == "data":
			var request struct {
				Reason string `json:"reason"`
			}
			if err := decodeSessionJSON(w, r, &request); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			status, err := service.RequestDeletion(r.Context(), claims.Subject, request.Reason)
			if err != nil {
				http.Error(w, "privacy deletion unavailable", http.StatusInternalServerError)
				return
			}
			go func(userID string) {
				_ = service.ProcessDeletion(context.Background(), userID)
			}(claims.Subject)
			writeJSON(w, http.StatusAccepted, map[string]any{"jobId": status.JobID, "status": status.Status})
		default:
			http.NotFound(w, r)
		}
	}
}

func authenticateUser(r *http.Request, manager *auth.Manager) (auth.Claims, error) {
	token := auth.BearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return auth.Claims{}, auth.ErrInvalidToken
	}
	return manager.Authenticate(r.Context(), token)
}

func writePrivacyError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, privacy.ErrDeletionInProgress):
		status = http.StatusConflict
	case errors.Is(err, privacy.ErrDataDeleted):
		status = http.StatusGone
	}
	http.Error(w, err.Error(), status)
}

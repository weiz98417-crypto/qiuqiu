package main

// server-residual-polish 1.2：writeMatchStateError 表驱动——NotFound/Conflict/
// 其他 + directordraft 变体（草稿输入/未配置/上游故障）。映射逻辑此前散在
// match_operator_api.go 的 9 处内联 if/else。

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"qiuqiu/internal/asr"
	"qiuqiu/internal/directordraft"
	"qiuqiu/internal/matchstate"
)

func TestWriteMatchStateErrorMapsDomainErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"not found", matchstate.ErrNotFound, http.StatusNotFound},
		{"conflict", matchstate.ErrConflict, http.StatusConflict},
		{"clock version conflict", matchstate.ErrClockVersionConflict, http.StatusConflict},
		{"wrapped not found", fmt.Errorf("facts: %w", matchstate.ErrNotFound), http.StatusNotFound},
		{"other domain error", matchstate.ErrInvalid, http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeMatchStateError(recorder, testCase.err)
			if recorder.Code != testCase.status {
				t.Fatalf("status = %d, want %d", recorder.Code, testCase.status)
			}
			if matchStateErrorStatus(testCase.err) != testCase.status {
				t.Fatalf("matchStateErrorStatus = %d, want %d", matchStateErrorStatus(testCase.err), testCase.status)
			}
			if matchStateWriteError(testCase.err).Error() == "" {
				t.Fatal("matchStateWriteError lost the message")
			}
		})
	}
}

func TestDirectorDraftErrorStatusVariants(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"no input", directordraft.ErrNoInput, http.StatusBadRequest},
		{"invalid transcript", directordraft.ErrInvalidTranscript, http.StatusBadRequest},
		{"draft not configured", directordraft.ErrNotConfigured, http.StatusServiceUnavailable},
		{"asr not configured", asr.ErrNotConfigured, http.StatusServiceUnavailable},
		{"upstream failure", errors.New("llm unavailable"), http.StatusBadGateway},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if status := directorDraftErrorStatus(testCase.err); status != testCase.status {
				t.Fatalf("status = %d, want %d", status, testCase.status)
			}
		})
	}
}

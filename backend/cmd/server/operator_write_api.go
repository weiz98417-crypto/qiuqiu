package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"qiuqiu/internal/operatorwrite"
)

type operatorHTTPError struct {
	Status  int
	Message string
}

func (e *operatorHTTPError) Error() string {
	return e.Message
}

func selectedOperatorWriteService(services []*operatorwrite.Service) *operatorwrite.Service {
	if len(services) > 0 && services[0] != nil {
		return services[0]
	}
	return operatorwrite.NewMemoryService()
}

func decodeOperatorJSON(w http.ResponseWriter, r *http.Request, destination any) ([]byte, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		body = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(destination); err != nil {
		return nil, err
	}
	return body, nil
}

func executeOperatorWrite(w http.ResponseWriter, r *http.Request, service *operatorwrite.Service, matchID, operation string, body []byte, callback operatorwrite.Operation) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	payloadHash, err := operatorwrite.PayloadHash(r.Method, r.URL.Path, body)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	response, replayed, err := service.Execute(r.Context(), operatorwrite.Request{
		MatchID:     matchID,
		Key:         key,
		PayloadHash: payloadHash,
		Operation:   operation,
	}, func(operationCtx context.Context) (operatorwrite.Response, error) {
		response, operationErr := callback(operationCtx)
		var httpErr *operatorHTTPError
		if errors.As(operationErr, &httpErr) {
			return operatorwrite.JSONResponse(httpErr.Status, map[string]string{"error": httpErr.Message})
		}
		return response, operationErr
	})
	if err != nil {
		switch {
		case errors.Is(err, operatorwrite.ErrKeyRequired):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, operatorwrite.ErrConflict), errors.Is(err, operatorwrite.ErrInProgress):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, "operator write unavailable", http.StatusInternalServerError)
		}
		return
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(response.Body)
}

func operatorError(status int, err error) error {
	return &operatorHTTPError{Status: status, Message: err.Error()}
}

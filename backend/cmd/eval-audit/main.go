package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/evals"
)

func main() {
	baseURL := flag.String("base-url", "http://localhost:8080", "QiuQiu backend URL")
	matchID := flag.String("match-id", "test", "match to audit")
	limit := flag.Int("limit", 100, "maximum recent traces to audit")
	out := flag.String("out", "", "optional JSON report path")
	failOn := flag.String("fail-on", "blocker", "blocker, warning, or never")
	flag.Parse()

	token := strings.TrimSpace(os.Getenv("APP_TOKEN"))
	traces, err := fetchTraces(*baseURL, *matchID, token, *limit)
	if err != nil {
		fail(err)
	}
	report := evals.AuditTraces(traces)
	if err := writeJSON(*out, report); err != nil {
		fail(err)
	}
	if (*failOn == "blocker" && report.Blockers > 0) || (*failOn == "warning" && len(report.Issues) > 0) {
		os.Exit(1)
	}
}

func fetchTraces(baseURL, matchID, token string, limit int) ([]companion.Trace, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, err
	}
	parsed.Path = parsed.Path + "/api/matches/" + url.PathEscape(matchID) + "/traces"
	query := parsed.Query()
	query.Set("limit", fmt.Sprintf("%d", limit))
	parsed.RawQuery = query.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	request, err := http.NewRequest(http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trace endpoint returned %s", response.Status)
	}
	var payload struct {
		Traces []companion.Trace `json:"traces"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Traces, nil
}

func writeJSON(path string, value any) error {
	var target *os.File
	if path == "" {
		target = os.Stdout
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		defer file.Close()
		target = file
	}
	encoder := json.NewEncoder(target)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "eval-audit:", err)
	os.Exit(2)
}

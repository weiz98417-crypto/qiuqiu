package memory

// Memobase adapter speaking the exact HTTP surface of the official Go SDK
// (github.com/memodb-io/memobase/src/client/memobase-go) with standard-library
// calls only: importing the SDK module would require a go.sum entry and this
// machine has no Go toolchain to regenerate it.
//
// Endpoints verified against the SDK sources:
//   https://github.com/memodb-io/memobase/blob/main/src/client/memobase-go/core/client.go
//   https://github.com/memodb-io/memobase/blob/main/src/client/memobase-go/core/user.go
//   https://github.com/memodb-io/memobase/blob/main/src/client/memobase-go/blob/blob.go
//   https://github.com/memodb-io/memobase/blob/main/src/client/memobase-go/network/network.go
//
// BaseURL = <project URL> + "/api/v1"; every request carries
// "Authorization: Bearer <token>" (core/client.go authTransport).
//
//	healthcheck  GET  {base}/healthcheck
//	get user     GET  {base}/users/{id}
//	create user  POST {base}/users                          body {"data":{},"id":"..."}
//	insert blob  POST {base}/blobs/insert/{id}?wait_process=b   body {"blob_type":"chat","blob_data":{"messages":[...]},"fields":{...}}
//	flush        POST {base}/users/buffer/{id}/chat?wait_process=b
//	profile      GET  {base}/users/profile/{id}?max_token_size=n&prefer_topics=t
//	profile PUT  {base}/users/profile/{id}/{profileID}      body {"content":"...","attributes":{"topic":"...","sub_topic":"..."}}
//	profile DEL  {base}/users/profile/{id}/{profileID}
//
// Responses use the envelope {"data":...,"errmsg":"...","errno":0}; HTTP >= 400
// or errno != 0 is an error (network.UnpackResponse). The Go SDK Profile op has
// no need_json parameter — the query params listed above are the complete set
// the SDK sends.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const memobaseAPIVersion = "api/v1"

const (
	defaultRecallLimit       = 5
	maxRecallLimit           = 10
	profileTokenSize         = 1000
	maxPortraitEntries       = 12
	maxPortraitRunes         = 480
	maxRecallBlockRunes      = 480
	maxResponseBodyBytes     = 1 << 20
	defaultExtractionTimeout = 10 * time.Second
)

type MemobaseConfig struct {
	// BaseURL is the Memobase API root without the /api/v1 suffix,
	// e.g. http://memobase:8000 or http://localhost:8019.
	BaseURL string
	// Token is the server ACCESS_TOKEN; empty disables the adapter entirely
	// (Configured() == false) so dev setups without Memobase stay silent.
	Token string
	// Timeout bounds each HTTP call including flush's LLM extraction wait.
	Timeout time.Duration
}

type Memobase struct {
	baseURL     string
	token       string
	httpClient  *http.Client
	degraded    atomic.Bool
	userCacheMu sync.Mutex
	userCache   map[string]string
}

func NewMemobase(cfg MemobaseConfig) *Memobase {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultExtractionTimeout
	}
	return &Memobase{
		baseURL:    strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		token:      strings.TrimSpace(cfg.Token),
		httpClient: &http.Client{Timeout: timeout},
		userCache:  make(map[string]string),
	}
}

// Configured reports whether a Memobase endpoint and token are set.
func (m *Memobase) Configured() bool {
	return m != nil && m.baseURL != "" && m.token != ""
}

// Degraded reports whether the last exchange with Memobase failed with a
// transport error or 5xx; recall paths check it to fall back to read_recent.
func (m *Memobase) Degraded() bool {
	return m == nil || m.degraded.Load()
}

// Observe inserts the moment as a chat blob. Extraction happens server-side
// on flush; the queue drives Flush so a batch of moments costs one call.
func (m *Memobase) Observe(ctx context.Context, moment Moment) error {
	if !m.Configured() {
		return ErrUnavailable
	}
	payload, err := m.chatBlobPayload(moment)
	if err != nil {
		return err
	}
	return m.insertPayload(ctx, moment.UserID, payload)
}

// Flush triggers server-side extraction of the user's buffered chat blobs.
func (m *Memobase) Flush(ctx context.Context, userID string) error {
	if !m.Configured() {
		return ErrUnavailable
	}
	remote, err := m.ensureUser(ctx, userID)
	if err != nil {
		return err
	}
	query := url.Values{"wait_process": {"true"}}
	_, err = m.call(ctx, http.MethodPost, "/users/buffer/"+remote+"/chat", query, nil)
	return err
}

// Recall renders profile entries as provenance-cited memories. Entries are
// scored by relevance to the focus, importance (fixed: profiles are synthesis,
// not raw moments) and recency, then cut to the top-k.
func (m *Memobase) Recall(ctx context.Context, query Query) []Recall {
	if !m.Configured() || m.Degraded() {
		return nil
	}
	entries, err := m.profileEntries(ctx, query.UserID)
	if err != nil {
		return nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = defaultRecallLimit
	}
	if limit > maxRecallLimit {
		limit = maxRecallLimit
	}
	scored := make([]Recall, 0, len(entries))
	for _, entry := range entries {
		content := strings.TrimSpace(entry.Content)
		if content == "" {
			continue
		}
		scored = append(scored, Recall{
			Content:    content,
			Importance: recallScore(query.Focus, entry),
			OccurredAt: entry.updatedAt(),
			Source:     profileSource(entry),
		})
	}
	sort.SliceStable(scored, func(left, right int) bool {
		if scored[left].Importance != scored[right].Importance {
			return scored[left].Importance > scored[right].Importance
		}
		return scored[left].OccurredAt.After(scored[right].OccurredAt)
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored
}

// Portrait renders the Memobase profile as a bounded Chinese context block
// plus the structured entries the C3 user page reads and edits.
func (m *Memobase) Portrait(ctx context.Context, userID string) (Portrait, error) {
	if !m.Configured() {
		return Portrait{}, ErrUnavailable
	}
	if m.Degraded() {
		return Portrait{}, ErrUnavailable
	}
	profiles, err := m.profileEntries(ctx, userID)
	if err != nil {
		return Portrait{}, err
	}
	entries := make([]PortraitEntry, 0, len(profiles))
	var updatedAt time.Time
	for _, profile := range profiles {
		content := strings.TrimSpace(profile.Content)
		if content == "" {
			continue
		}
		stamped := profile.updatedAt()
		if stamped.After(updatedAt) {
			updatedAt = stamped
		}
		entries = append(entries, PortraitEntry{
			ID:        profile.ID,
			Topic:     profile.Attributes.Topic,
			SubTopic:  profile.Attributes.SubTopic,
			Content:   content,
			UpdatedAt: stamped,
			Source:    PortraitSourceSynthesis,
		})
	}
	if len(entries) == 0 {
		return Portrait{}, nil
	}
	return Portrait{Block: RenderPortraitBlock(entries, updatedAt), Entries: entries, UpdatedAt: updatedAt}, nil
}

// UpdateProfileEntry PUTs a user edit onto the remote profile slot (official
// SDK core/user.go UpdateProfile). Requires the Memobase profile id from the
// last portrait read; the local overlay stays the authority.
func (m *Memobase) UpdateProfileEntry(ctx context.Context, userID, entryID, topic, subTopic, content string) error {
	if !m.Configured() {
		return ErrUnavailable
	}
	remote, err := m.ensureUser(ctx, userID)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{
		"content":    content,
		"attributes": map[string]string{"topic": topic, "sub_topic": subTopic},
	})
	if err != nil {
		return err
	}
	_, err = m.call(ctx, http.MethodPut, "/users/profile/"+remote+"/"+url.PathEscape(entryID), nil, body)
	return err
}

// DeleteProfileEntry DELETEs the remote profile slot (official SDK
// core/user.go DeleteProfile). Requires the Memobase profile id.
func (m *Memobase) DeleteProfileEntry(ctx context.Context, userID, entryID string) error {
	if !m.Configured() {
		return ErrUnavailable
	}
	remote, err := m.ensureUser(ctx, userID)
	if err != nil {
		return err
	}
	_, err = m.call(ctx, http.MethodDelete, "/users/profile/"+remote+"/"+url.PathEscape(entryID), nil, nil)
	return err
}

var _ ProfileEntryMutator = (*Memobase)(nil)

// Threads stays unsupported on Memobase: the open-thread ledger is a local
// table (C2), not Memobase synthesis.
func (m *Memobase) Threads(ctx context.Context, userID string) ([]Thread, error) {
	return nil, ErrNotSupported
}

// chatBlobPayload builds the insert request body; the queue stores it
// verbatim in memory_backlog so replay is a byte-for-byte retry.
func (m *Memobase) chatBlobPayload(moment Moment) ([]byte, error) {
	occurred := moment.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	return json.Marshal(map[string]any{
		"blob_type": "chat",
		"blob_data": map[string]any{
			"messages": []chatMessage{{
				Role:      "user",
				Content:   moment.Content,
				Alias:     "用户",
				CreatedAt: occurred.UTC().Format(time.RFC3339),
			}},
		},
		"fields": map[string]any{
			"kind":            string(moment.Kind),
			"importance":      moment.Importance,
			"ledger_sequence": moment.LedgerSequence,
		},
	})
}

// insertPayload POSTs a previously built chat blob body, re-ensuring the user
// exists so backlog replay also recovers from user recreation.
func (m *Memobase) insertPayload(ctx context.Context, userID string, payload []byte) error {
	remote, err := m.ensureUser(ctx, userID)
	if err != nil {
		return err
	}
	query := url.Values{"wait_process": {"false"}}
	_, err = m.call(ctx, http.MethodPost, "/blobs/insert/"+remote, query, payload)
	return err
}

// memobaseUserID namespaces remote users so a shared Memobase server never
// collides with other tenants, and path-escapes the id once so colon- or
// slash-bearing local user ids stay a single URL path segment everywhere.
func memobaseUserID(userID string) string {
	return url.PathEscape("qiuqiu-" + userID)
}

func (m *Memobase) ensureUser(ctx context.Context, userID string) (string, error) {
	m.userCacheMu.Lock()
	remote, cached := m.userCache[userID]
	m.userCacheMu.Unlock()
	if cached {
		return remote, nil
	}
	remote = memobaseUserID(userID)
	if _, err := m.call(ctx, http.MethodGet, "/users/"+remote, nil, nil); err == nil {
		m.cacheUser(userID, remote)
		return remote, nil
	}
	body, err := json.Marshal(map[string]any{"data": map[string]any{}, "id": remote})
	if err != nil {
		return "", err
	}
	if _, err := m.call(ctx, http.MethodPost, "/users", nil, body); err != nil {
		return "", err
	}
	m.cacheUser(userID, remote)
	return remote, nil
}

func (m *Memobase) cacheUser(userID, remote string) {
	m.userCacheMu.Lock()
	m.userCache[userID] = remote
	m.userCacheMu.Unlock()
}

func (m *Memobase) profileEntries(ctx context.Context, userID string) ([]profileEntry, error) {
	remote, err := m.ensureUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	query := url.Values{"max_token_size": {strconv.Itoa(profileTokenSize)}}
	envelope, err := m.call(ctx, http.MethodGet, "/users/profile/"+remote, query, nil)
	if err != nil {
		return nil, err
	}
	var data struct {
		Profiles []profileEntry `json:"profiles"`
	}
	if len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return nil, fmt.Errorf("memobase: decode profile: %w", err)
		}
	}
	return data.Profiles, nil
}

type chatMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Alias     string `json:"alias,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// profileEntry mirrors the SDK's UserProfileData without its uuid.UUID
// dependency (stdlib-only package).
type profileEntry struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	Attributes struct {
		Topic    string `json:"topic"`
		SubTopic string `json:"sub_topic"`
	} `json:"attributes"`
	UpdatedAt string `json:"updated_at"`
	CreatedAt string `json:"created_at"`
}

var profileTimeFormats = []string{
	time.RFC3339,
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999",
	"2006-01-02 15:04:05",
}

func (e profileEntry) updatedAt() time.Time {
	return parseMemobaseTime(e.UpdatedAt)
}

func parseMemobaseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, format := range profileTimeFormats {
		if stamped, err := time.Parse(format, value); err == nil {
			return stamped
		}
	}
	return time.Time{}
}

func profileSource(entry profileEntry) string {
	return "memobase://profile/" + entry.Attributes.Topic + "/" + entry.Attributes.SubTopic
}

// recallScore blends relevance (focus match) with a fixed importance and a
// recency bonus; deterministic so recall blocks are reproducible in evals.
func recallScore(focus string, entry profileEntry) float64 {
	score := 0.5
	focus = strings.TrimSpace(focus)
	if focus != "" {
		if strings.Contains(focus, entry.Attributes.Topic) ||
			strings.Contains(focus, entry.Attributes.SubTopic) ||
			strings.Contains(entry.Content, focus) {
			score += 0.3
		}
	}
	age := time.Since(entry.updatedAt())
	switch {
	case age >= 0 && age <= 24*time.Hour:
		score += 0.2
	case age > 24*time.Hour && age <= 7*24*time.Hour:
		score += 0.1
	}
	return clamp01(score)
}

type memobaseResponse struct {
	Data   json.RawMessage `json:"data"`
	Errmsg string          `json:"errmsg"`
	Errno  int             `json:"errno"`
}

func (m *Memobase) call(ctx context.Context, method, path string, query url.Values, body []byte) (memobaseResponse, error) {
	endpoint := m.baseURL + "/" + memobaseAPIVersion + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return memobaseResponse{}, fmt.Errorf("memobase: build %s %s: %w", method, path, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+m.token)
	response, err := m.httpClient.Do(request)
	if err != nil {
		m.degraded.Store(true)
		return memobaseResponse{}, fmt.Errorf("memobase %s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBodyBytes))
	if err != nil {
		m.degraded.Store(true)
		return memobaseResponse{}, fmt.Errorf("memobase %s %s: read body: %w", method, path, err)
	}
	if response.StatusCode >= 500 {
		m.degraded.Store(true)
		return memobaseResponse{}, fmt.Errorf("memobase %s %s: status %d: %s", method, path, response.StatusCode, truncateForLog(payload))
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		m.degraded.Store(true)
		return memobaseResponse{}, fmt.Errorf("memobase %s %s: status %d", method, path, response.StatusCode)
	}
	if response.StatusCode >= 400 {
		// 404 on user lookup is an expected branch of ensureUser and must
		// not mark the adapter degraded.
		return memobaseResponse{}, fmt.Errorf("memobase %s %s: status %d: %s", method, path, response.StatusCode, truncateForLog(payload))
	}
	var envelope memobaseResponse
	if err := json.Unmarshal(payload, &envelope); err != nil {
		m.degraded.Store(true)
		return memobaseResponse{}, fmt.Errorf("memobase %s %s: decode: %w", method, path, err)
	}
	if envelope.Errno != 0 {
		return memobaseResponse{}, fmt.Errorf("memobase %s %s: errno %d: %s", method, path, envelope.Errno, envelope.Errmsg)
	}
	m.degraded.Store(false)
	return envelope, nil
}

func truncateForLog(payload []byte) string {
	const limit = 200
	runes := []rune(string(payload))
	if len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return string(runes)
}

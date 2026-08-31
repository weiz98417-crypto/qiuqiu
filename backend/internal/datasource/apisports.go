package datasource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	baseURL                 = "https://v3.football.api-sports.io"
	maxAPIErrorMessageBytes = 4096
)

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) WithBaseURL(url string) *Client {
	if value := strings.TrimRight(strings.TrimSpace(url), "/"); value != "" {
		c.baseURL = value
	}
	return c
}

// --- Response wrapper ---

type apiResponse struct {
	Get      string          `json:"get"`
	Results  int             `json:"results"`
	Errors   json.RawMessage `json:"errors"`
	Response json.RawMessage `json:"response"`
}

// --- Fixture (match) types ---

type FixtureWrapper struct {
	Fixture FixtureInfo `json:"fixture"`
	League  LeagueInfo  `json:"league"`
	Teams   TeamsInfo   `json:"teams"`
	Goals   GoalsInfo   `json:"goals"`
}

type FixtureInfo struct {
	ID     int           `json:"id"`
	Date   string        `json:"date"`
	Status FixtureStatus `json:"status"`
}

type FixtureStatus struct {
	Long    string `json:"long"`
	Short   string `json:"short"`
	Elapsed int    `json:"elapsed"`
}

type LeagueInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type TeamsInfo struct {
	Home TeamRef `json:"home"`
	Away TeamRef `json:"away"`
}

type TeamRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Logo string `json:"logo"`
}

type GoalsInfo struct {
	Home *int `json:"home"`
	Away *int `json:"away"`
}

// Fixture is the public-facing flat fixture (for backward compat).
type Fixture struct {
	ID            int
	HomeTeam      string
	AwayTeam      string
	Competition   string
	KickoffAt     time.Time
	HomeGoal      int
	AwayGoal      int
	HomeGoalKnown bool
	AwayGoalKnown bool
	Status        string
	Elapsed       int
	Source        string
	Freshness     string
}

func (fw FixtureWrapper) ToFixture() Fixture {
	f := Fixture{
		ID:          fw.Fixture.ID,
		HomeTeam:    fw.Teams.Home.Name,
		AwayTeam:    fw.Teams.Away.Name,
		Competition: fw.League.Name,
		Status:      fw.Fixture.Status.Short,
		Elapsed:     fw.Fixture.Status.Elapsed,
		Source:      "api-sports",
		Freshness:   "fresh",
	}
	if kickoff, err := time.Parse(time.RFC3339, fw.Fixture.Date); err == nil {
		f.KickoffAt = kickoff
	}
	if fw.Goals.Home != nil {
		f.HomeGoal = *fw.Goals.Home
		f.HomeGoalKnown = true
	}
	if fw.Goals.Away != nil {
		f.AwayGoal = *fw.Goals.Away
		f.AwayGoalKnown = true
	}
	return f
}

// --- Event types ---

type Event struct {
	Time     EventTime `json:"time"`
	Team     TeamRef   `json:"team"`
	Player   PlayerRef `json:"player"`
	Assist   PlayerRef `json:"assist"`
	Type     string    `json:"type"`
	Detail   string    `json:"detail"`
	Comments *string   `json:"comments"`
}

type EventTime struct {
	Elapsed int  `json:"elapsed"`
	Extra   *int `json:"extra"`
}

type PlayerRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// --- API methods ---

func (c *Client) GetLiveFixtures() ([]Fixture, error) {
	var wrappers []FixtureWrapper
	if err := c.doRequest("/fixtures?live=all", &wrappers); err != nil {
		return nil, err
	}
	fixtures := make([]Fixture, len(wrappers))
	for i, w := range wrappers {
		fixtures[i] = w.ToFixture()
	}
	return fixtures, nil
}

func (c *Client) GetTodayFixtures() ([]Fixture, error) {
	return c.GetTodayFixturesContext(context.Background())
}

func (c *Client) GetTodayFixturesContext(ctx context.Context) ([]Fixture, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return c.GetFixturesContext(ctx, start, start.AddDate(0, 0, 1), now.Location().String())
}

func (c *Client) GetFixtures(from, to time.Time, timezone string) ([]Fixture, error) {
	return c.GetFixturesContext(context.Background(), from, to, timezone)
}

func (c *Client) GetFixturesContext(ctx context.Context, from, to time.Time, timezone string) ([]Fixture, error) {
	location := fixtureLocation(from, timezone)
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if from.IsZero() {
		from = time.Now().In(location)
	}
	if to.IsZero() || !to.After(from) {
		to = from.AddDate(0, 0, 1)
	}
	start := from.In(location)
	end := to.In(location)
	fixturesByID := make(map[int]Fixture)
	for date := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location); date.Before(end); date = date.AddDate(0, 0, 1) {
		fixtures, err := c.getFixturesForDate(ctx, date.Format("2006-01-02"))
		if err != nil {
			return nil, err
		}
		for _, fixture := range fixtures {
			if !fixture.KickoffAt.IsZero() {
				fixture.KickoffAt = fixture.KickoffAt.In(location)
			}
			fixturesByID[fixture.ID] = fixture
		}
	}
	fixtures := make([]Fixture, 0, len(fixturesByID))
	for _, fixture := range fixturesByID {
		fixtures = append(fixtures, fixture)
	}
	sort.Slice(fixtures, func(left, right int) bool {
		if fixtures[left].KickoffAt.Equal(fixtures[right].KickoffAt) {
			return fixtures[left].ID < fixtures[right].ID
		}
		if fixtures[left].KickoffAt.IsZero() {
			return false
		}
		if fixtures[right].KickoffAt.IsZero() {
			return true
		}
		return fixtures[left].KickoffAt.Before(fixtures[right].KickoffAt)
	})
	return fixtures, nil
}

func fixtureLocation(from time.Time, timezone string) *time.Location {
	location := from.Location()
	if from.IsZero() || location == nil {
		location = time.Local
	}
	if name := strings.TrimSpace(timezone); name != "" {
		if loaded, err := time.LoadLocation(name); err == nil {
			return loaded
		}
		normalized := strings.ToUpper(name)
		if strings.HasPrefix(normalized, "UTC") {
			if parsed, err := time.Parse("Z07:00", strings.TrimPrefix(normalized, "UTC")); err == nil {
				_, offsetSeconds := parsed.Zone()
				return time.FixedZone(normalized, offsetSeconds)
			}
		}
	}
	return location
}

func (c *Client) getFixturesForDate(ctx context.Context, date string) ([]Fixture, error) {
	var wrappers []FixtureWrapper
	if err := c.doRequestContext(ctx, "/fixtures?date="+date, &wrappers); err != nil {
		return nil, err
	}
	fixtures := make([]Fixture, len(wrappers))
	for i, w := range wrappers {
		fixtures[i] = w.ToFixture()
	}
	return fixtures, nil
}

func (c *Client) GetEvents(fixtureID int) ([]Event, error) {
	url := fmt.Sprintf("/fixtures/events?fixture=%d", fixtureID)
	var events []Event
	if err := c.doRequest(url, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func (c *Client) doRequest(path string, result interface{}) error {
	return c.doRequestContext(context.Background(), path, result)
}

func (c *Client) doRequestContext(ctx context.Context, path string, result interface{}) error {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-apisports-key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("api request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxAPIErrorMessageBytes+1))
		if readErr != nil {
			return fmt.Errorf("api request failed with status %d: read error body: %w", resp.StatusCode, readErr)
		}
		return fmt.Errorf("api request failed with status %d: %s", resp.StatusCode, boundedAPIErrorText(body))
	}

	var wrapper apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if message := apiResponseError(wrapper.Errors); message != "" {
		return fmt.Errorf("api response error: %s", message)
	}

	if wrapper.Results == 0 {
		return nil
	}

	return json.Unmarshal(wrapper.Response, result)
}

func apiResponseError(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "{}" || trimmed == "[]" || trimmed == `""` {
		return ""
	}
	var message string
	if json.Unmarshal(raw, &message) == nil {
		return boundedAPIErrorText([]byte(message))
	}
	var compact bytes.Buffer
	if json.Compact(&compact, raw) == nil {
		return boundedAPIErrorText([]byte(compact.String()))
	}
	return boundedAPIErrorText(raw)
}

func boundedAPIErrorText(body []byte) string {
	truncated := len(body) > maxAPIErrorMessageBytes
	if truncated {
		body = body[:maxAPIErrorMessageBytes]
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(http.StatusBadGateway)
	}
	if truncated {
		message += "…"
	}
	return message
}

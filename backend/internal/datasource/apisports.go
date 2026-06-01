package datasource

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const baseURL = "https://v3.football.api-sports.io"

type Client struct {
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// --- Response wrapper ---

type apiResponse struct {
	Get      string          `json:"get"`
	Results  int             `json:"results"`
	Errors   interface{}     `json:"errors"`
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
	ID       int
	HomeTeam string
	AwayTeam string
	HomeGoal int
	AwayGoal int
	Status   string
	Elapsed  int
}

func (fw FixtureWrapper) ToFixture() Fixture {
	f := Fixture{
		ID:       fw.Fixture.ID,
		HomeTeam: fw.Teams.Home.Name,
		AwayTeam: fw.Teams.Away.Name,
		Status:   fw.Fixture.Status.Short,
		Elapsed:  fw.Fixture.Status.Elapsed,
	}
	if fw.Goals.Home != nil {
		f.HomeGoal = *fw.Goals.Home
	}
	if fw.Goals.Away != nil {
		f.AwayGoal = *fw.Goals.Away
	}
	return f
}

// --- Event types ---

type Event struct {
	Time     EventTime  `json:"time"`
	Team     TeamRef    `json:"team"`
	Player   PlayerRef  `json:"player"`
	Assist   PlayerRef  `json:"assist"`
	Type     string     `json:"type"`
	Detail   string     `json:"detail"`
	Comments *string    `json:"comments"`
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
	date := time.Now().Format("2006-01-02")
	var wrappers []FixtureWrapper
	if err := c.doRequest("/fixtures?date="+date, &wrappers); err != nil {
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
	req, err := http.NewRequest("GET", baseURL+path, nil)
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

	var wrapper apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if wrapper.Results == 0 {
		return nil
	}

	return json.Unmarshal(wrapper.Response, result)
}

package event

import "time"

// StandardEvent is the normalized match event from any data source.
type StandardEvent struct {
	MatchID   int64      `json:"match_id"` // fixture ID from api-sports
	ID        int64      `json:"id"`
	Type      string     `json:"type"` // goal|shot|yellow_card|red_card|penalty|foul|corner|offside|substitution|var_check|match_start|match_end
	Team      string     `json:"team"` // "home"|"away"
	Minute    int        `json:"minute"`
	Player    PlayerInfo `json:"player"`
	Score     ScoreInfo  `json:"score"`
	Timestamp time.Time  `json:"timestamp"`
}

type PlayerInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type ScoreInfo struct {
	Home int `json:"home"`
	Away int `json:"away"`
}

// Priority returns the event priority: P0=0 (critical), P1=1, P2=2, P3=3, P4=4 (drop).
func (e *StandardEvent) Priority() int {
	switch e.Type {
	case "goal", "red_card", "penalty", "match_start", "match_end":
		return 0
	case "shot", "yellow_card":
		return 1
	case "foul", "substitution", "var_check":
		return 2
	case "corner", "offside":
		return 3
	default:
		return 4
	}
}

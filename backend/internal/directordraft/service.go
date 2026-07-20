package directordraft

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"qiuqiu/internal/asr"
	"qiuqiu/internal/matchstate"
)

var (
	ErrNoInput       = errors.New("director draft input is empty")
	ErrNotConfigured = errors.New("director draft provider is not configured")
)

type Recognizer interface {
	Transcribe(context.Context, []byte, []string) (*asr.Result, error)
}

type Extractor interface {
	Extract(context.Context, string, MatchContext) (Extraction, error)
}

type Request struct {
	Text                 string `json:"text,omitempty"`
	AudioBase64          string `json:"audioBase64,omitempty"`
	AudioMIME            string `json:"audioMime,omitempty"`
	OccurredPeriod       string `json:"occurredPeriod,omitempty"`
	OccurredSeconds      *int   `json:"occurredSeconds,omitempty"`
	CapturedClockVersion *int64 `json:"capturedClockVersion,omitempty"`
}

type MatchContext struct {
	MatchID string
	Config  matchstate.MatchConfig
	Clock   matchstate.MatchClock
}

type Extraction struct {
	Team          string                 `json:"team"`
	EventType     string                 `json:"eventType"`
	Participants  []ExtractedParticipant `json:"participants"`
	Description   string                 `json:"description"`
	OccurredClock string                 `json:"occurredClock"`
	Confidence    float64                `json:"confidence"`
}

type ExtractedParticipant struct {
	Role string `json:"role"`
	Name string `json:"name"`
}

type DraftParticipant struct {
	Role     string `json:"role"`
	Name     string `json:"name"`
	TeamID   string `json:"teamId,omitempty"`
	TeamName string `json:"teamName,omitempty"`
	Resolved bool   `json:"resolved"`
}

type Draft struct {
	Source               string             `json:"source"`
	Transcript           string             `json:"transcript"`
	TeamID               string             `json:"teamId,omitempty"`
	TeamName             string             `json:"teamName,omitempty"`
	EventType            string             `json:"eventType,omitempty"`
	OccurredPeriod       string             `json:"occurredPeriod"`
	OccurredSeconds      int                `json:"occurredSeconds"`
	CapturedClockVersion int64              `json:"capturedClockVersion"`
	Participants         []DraftParticipant `json:"participants"`
	Description          string             `json:"description,omitempty"`
	InferredFields       []string           `json:"inferredFields"`
	FieldConfidence      map[string]float64 `json:"fieldConfidence"`
}

type Result struct {
	Transcript    string   `json:"transcript"`
	ASRProvider   string   `json:"asrProvider,omitempty"`
	ASRConfidence float64  `json:"asrConfidence,omitempty"`
	Draft         Draft    `json:"draft"`
	Warnings      []string `json:"warnings"`
	Ready         bool     `json:"ready"`
}

type Service struct {
	recognizer Recognizer
	extractor  Extractor
}

func NewService(recognizer Recognizer, extractor Extractor) *Service {
	return &Service{recognizer: recognizer, extractor: extractor}
}

func (service *Service) Build(ctx context.Context, request Request, match MatchContext) (Result, error) {
	transcript := strings.TrimSpace(request.Text)
	result := Result{}
	if transcript == "" {
		if service == nil || service.recognizer == nil {
			return Result{}, ErrNotConfigured
		}
		audio, err := decodeAudio(request.AudioBase64)
		if err != nil {
			return Result{}, err
		}
		asrResult, err := service.recognizer.Transcribe(ctx, audio, matchHints(match.Config))
		if err != nil {
			return Result{}, err
		}
		transcript = strings.TrimSpace(asrResult.Text)
		result.ASRProvider = asrResult.Provider
		result.ASRConfidence = asrResult.Confidence
	}
	if transcript == "" {
		return Result{}, ErrNoInput
	}
	if service == nil || service.extractor == nil {
		return Result{}, ErrNotConfigured
	}
	extraction, err := service.extractor.Extract(ctx, transcript, match)
	if err != nil {
		return Result{}, err
	}

	result.Transcript = transcript
	result.Draft = Draft{
		Source:               "operator_voice",
		Transcript:           transcript,
		EventType:            normalizeEventType(extraction.EventType),
		OccurredPeriod:       defaultString(match.Clock.Period, "pre_match"),
		OccurredSeconds:      match.Clock.CurrentElapsed(time.Now()),
		CapturedClockVersion: match.Clock.Version,
		Participants:         []DraftParticipant{},
		Description:          strings.TrimSpace(extraction.Description),
		InferredFields:       []string{},
		FieldConfidence:      map[string]float64{},
	}
	if result.Draft.EventType != "" {
		result.Draft.InferredFields = append(result.Draft.InferredFields, "eventType")
	}
	if result.Draft.Description != "" {
		result.Draft.InferredFields = append(result.Draft.InferredFields, "description")
	}
	if validClockPeriod(request.OccurredPeriod) {
		result.Draft.OccurredPeriod = request.OccurredPeriod
	}
	if request.OccurredSeconds != nil && *request.OccurredSeconds >= 0 && *request.OccurredSeconds <= 6*60*60 {
		result.Draft.OccurredSeconds = *request.OccurredSeconds
	}
	if request.CapturedClockVersion != nil && *request.CapturedClockVersion >= 0 {
		result.Draft.CapturedClockVersion = *request.CapturedClockVersion
	}
	result.Draft.TeamID, result.Draft.TeamName = resolveTeam(extraction.Team, match.Config)
	if result.Draft.TeamID != "" {
		result.Draft.InferredFields = append(result.Draft.InferredFields, "teamId")
	} else if strings.TrimSpace(extraction.Team) != "" {
		result.Warnings = append(result.Warnings, fmt.Sprintf("无法匹配球队：%s", strings.TrimSpace(extraction.Team)))
	}
	if clock, ok := parseClock(extraction.OccurredClock); ok {
		result.Draft.OccurredSeconds = clock
		result.Draft.InferredFields = append(result.Draft.InferredFields, "occurredSeconds")
	}

	for _, participant := range extraction.Participants {
		role := strings.TrimSpace(participant.Role)
		name := strings.TrimSpace(participant.Name)
		if role == "" || name == "" {
			continue
		}
		teamID, teamName, resolved := resolvePlayer(name, match.Config)
		result.Draft.Participants = append(result.Draft.Participants, DraftParticipant{
			Role: role, Name: name, TeamID: teamID, TeamName: teamName, Resolved: resolved,
		})
		if !resolved {
			result.Warnings = append(result.Warnings, fmt.Sprintf("待确认球员：%s", name))
			continue
		}
		if result.Draft.TeamID == "" {
			result.Draft.TeamID = teamID
			result.Draft.TeamName = teamName
		} else if teamID != "" && teamID != result.Draft.TeamID {
			result.Warnings = append(result.Warnings, fmt.Sprintf("球员 %s 不属于所选球队", name))
		}
	}
	if len(result.Draft.Participants) > 0 {
		result.Draft.InferredFields = append(result.Draft.InferredFields, "participants")
	}
	result.Warnings = uniqueStrings(result.Warnings)
	result.Draft.InferredFields = uniqueStrings(result.Draft.InferredFields)
	if extraction.Confidence > 0 {
		for _, field := range result.Draft.InferredFields {
			result.Draft.FieldConfidence[field] = extraction.Confidence
		}
		if extraction.Confidence < 0.65 {
			result.Warnings = append(result.Warnings, "模型置信度较低，请核对所有字段")
			result.Warnings = uniqueStrings(result.Warnings)
		}
	}
	result.Ready = draftReady(result.Draft, result.Warnings)
	return result, nil
}

func decodeAudio(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, ErrNoInput
	}
	if comma := strings.IndexByte(value, ','); strings.HasPrefix(value, "data:") && comma >= 0 {
		value = value[comma+1:]
	}
	audio, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(audio) == 0 {
		return nil, fmt.Errorf("%w: invalid audio", ErrNoInput)
	}
	return audio, nil
}

func matchHints(config matchstate.MatchConfig) []string {
	hints := []string{config.HomeTeam, config.AwayTeam}
	for _, player := range append(append([]matchstate.Player{}, config.HomePlayers...), config.AwayPlayers...) {
		hints = append(hints, player.Name)
	}
	return uniqueStrings(hints)
}

func resolveTeam(value string, config matchstate.MatchConfig) (string, string) {
	value = strings.TrimSpace(value)
	switch {
	case strings.EqualFold(value, "home"), value == "主队", strings.EqualFold(value, config.HomeTeam):
		return "home", config.HomeTeam
	case strings.EqualFold(value, "away"), value == "客队", strings.EqualFold(value, config.AwayTeam):
		return "away", config.AwayTeam
	default:
		return "", ""
	}
}

func resolvePlayer(name string, config matchstate.MatchConfig) (string, string, bool) {
	for _, player := range config.HomePlayers {
		if strings.EqualFold(strings.TrimSpace(player.Name), name) {
			return "home", config.HomeTeam, true
		}
	}
	for _, player := range config.AwayPlayers {
		if strings.EqualFold(strings.TrimSpace(player.Name), name) {
			return "away", config.AwayTeam, true
		}
	}
	return "", "", false
}

func normalizeEventType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	if allowedEventTypes[value] {
		return value
	}
	return ""
}

func draftReady(draft Draft, warnings []string) bool {
	if draft.EventType == "" || draft.Description == "" || len(warnings) > 0 {
		return false
	}
	required := requiredRoles[draft.EventType]
	for _, role := range required {
		found := false
		for _, participant := range draft.Participants {
			if participant.Role == role && participant.Resolved {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if requiresTeam[draft.EventType] && draft.TeamID == "" {
		return false
	}
	return true
}

func parseClock(value string) (int, bool) {
	var minutes, seconds int
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d:%d", &minutes, &seconds); err != nil || minutes < 0 || seconds < 0 || seconds > 59 {
		return 0, false
	}
	return minutes*60 + seconds, true
}

func validClockPeriod(period string) bool {
	switch strings.TrimSpace(period) {
	case "pre_match", "first_half", "halftime", "second_half", "extra_time", "fulltime":
		return true
	default:
		return false
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

var allowedEventTypes = map[string]bool{
	"kickoff": true, "goal": true, "shot": true, "big_chance": true, "save": true,
	"miss": true, "foul": true, "yellow_card": true, "red_card": true, "var_check": true,
	"var_result": true, "goal_cancelled": true, "penalty": true, "penalty_awarded": true,
	"substitution": true, "injury": true, "tactical_shift": true, "pressure": true,
	"halftime": true, "fulltime": true, "match_end": true, "operator_note": true,
}

var requiredRoles = map[string][]string{
	"goal": {"scorer"}, "shot": {"shooter"}, "save": {"keeper"}, "miss": {"shooter"},
	"foul": {"offender"}, "yellow_card": {"offender"}, "red_card": {"offender"},
	"substitution": {"sub_on", "sub_off"}, "injury": {"injured"},
}

var requiresTeam = map[string]bool{
	"goal": true, "shot": true, "big_chance": true, "save": true, "miss": true,
	"foul": true, "yellow_card": true, "red_card": true, "penalty": true,
	"penalty_awarded": true, "substitution": true, "injury": true,
	"tactical_shift": true, "pressure": true,
}

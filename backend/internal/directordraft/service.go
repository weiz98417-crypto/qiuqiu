package directordraft

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"qiuqiu/internal/asr"
	"qiuqiu/internal/matchstate"
)

var (
	ErrNoInput           = errors.New("director draft input is empty")
	ErrInvalidTranscript = errors.New("语音转写不是赛事口述，请重试")
	ErrNotConfigured     = errors.New("director draft provider is not configured")
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

type Transcription struct {
	Transcript    string  `json:"transcript"`
	ASRProvider   string  `json:"asrProvider,omitempty"`
	ASRConfidence float64 `json:"asrConfidence,omitempty"`
}

type Service struct {
	recognizer Recognizer
	extractor  Extractor
}

func NewService(recognizer Recognizer, extractor Extractor) *Service {
	return &Service{recognizer: recognizer, extractor: extractor}
}

func (service *Service) Transcribe(ctx context.Context, request Request, config matchstate.MatchConfig) (Transcription, error) {
	transcript := strings.TrimSpace(request.Text)
	result := Transcription{}
	if transcript == "" {
		if service == nil || service.recognizer == nil {
			return Transcription{}, ErrNotConfigured
		}
		audio, err := decodeAudio(request.AudioBase64)
		if err != nil {
			return Transcription{}, err
		}
		asrResult, err := service.recognizer.Transcribe(ctx, audio, matchHints(config))
		if err != nil {
			return Transcription{}, err
		}
		transcript = strings.TrimSpace(asrResult.Text)
		result.ASRProvider = asrResult.Provider
		result.ASRConfidence = asrResult.Confidence
	}
	if transcript == "" {
		return Transcription{}, ErrNoInput
	}
	result.Transcript = transcript
	return result, nil
}

func (service *Service) Build(ctx context.Context, request Request, match MatchContext) (Result, error) {
	transcription, err := service.Transcribe(ctx, request, match.Config)
	if err != nil {
		return Result{}, err
	}
	transcript := transcription.Transcript
	result := Result{ASRProvider: transcription.ASRProvider, ASRConfidence: transcription.ASRConfidence}
	if isNonEventDescription(transcript) {
		return Result{}, ErrInvalidTranscript
	}
	if !isPlausibleMatchTranscript(transcript, match.Config) {
		return Result{}, ErrInvalidTranscript
	}
	if service == nil || service.extractor == nil {
		return Result{}, ErrNotConfigured
	}
	extraction, err := service.extractor.Extract(ctx, transcript, match)
	if err != nil {
		return Result{}, err
	}
	description := strings.TrimSpace(extraction.Description)
	if isNonEventDescription(description) {
		description = ""
		result.Warnings = append(result.Warnings, "语音描述不是比赛事实，已留空等待人工核对")
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
		Description:          description,
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

func isNonEventDescription(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	for _, marker := range []string{
		"unless there is a specific request",
		"citations should",
		"author, year",
		"page number",
		"citation",
		"bibliography",
		"除非有明确要求",
		"引用均按常规方式列出",
		"作者、年份和页码",
		"作者、年份、页码",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func isPlausibleMatchTranscript(value string, config matchstate.MatchConfig) bool {
	value = strings.TrimSpace(value)
	meaningfulRunes := 0
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			meaningfulRunes++
		}
	}
	if meaningfulRunes < 2 {
		return false
	}

	normalized := strings.ToLower(value)
	for _, hint := range matchHints(config) {
		hint = strings.TrimSpace(hint)
		if utf8RuneCount(hint) >= 2 && strings.Contains(normalized, strings.ToLower(hint)) {
			return true
		}
	}
	for _, keyword := range []string{
		"进球", "进了", "射门", "防守", "扑救", "换人", "犯规", "黄牌", "红牌", "点球", "助攻",
		"越位", "传球", "角球", "任意球", "门柱", "开球", "绝杀", "补时", "goal", "shoot",
		"shot", "save", "substitution", "foul", "yellow card", "red card", "penalty", "offside",
	} {
		if strings.Contains(normalized, keyword) {
			return true
		}
	}
	return false
}

func utf8RuneCount(value string) int {
	return len([]rune(value))
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

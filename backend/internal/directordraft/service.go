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
	extraction = enrichExtraction(extraction, transcript, match.Config)
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
	if result.Draft.TeamID == "" {
		result.Draft.TeamID, result.Draft.TeamName = inferTeamFromTranscript(transcript, match.Config)
		if result.Draft.TeamID != "" {
			result.Draft.InferredFields = append(result.Draft.InferredFields, "teamId")
		}
	}
	if clock, ok := parseClock(extraction.OccurredClock); ok {
		result.Draft.OccurredSeconds = clock
		result.Draft.InferredFields = append(result.Draft.InferredFields, "occurredSeconds")
	}

	for _, participant := range extraction.Participants {
		role := normalizeParticipantRole(participant.Role)
		name := strings.TrimSpace(participant.Name)
		if role == "" || name == "" {
			continue
		}
		teamID, teamName, canonicalName, resolved := resolvePlayer(name, match.Config)
		if resolved && canonicalName != "" {
			name = canonicalName
		}
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
		} else if teamID != "" && expectedParticipantTeam(result.Draft.EventType, role, result.Draft.TeamID) != teamID {
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
	normalized := normalizeLookup(value)
	withoutSuffix := strings.TrimSuffix(strings.TrimSuffix(normalized, "国家队"), "队")
	switch {
	case strings.EqualFold(value, "home"), value == "主队", strings.EqualFold(value, config.HomeTeam), teamLookupMatches(withoutSuffix, config.HomeTeam):
		return "home", config.HomeTeam
	case strings.EqualFold(value, "away"), value == "客队", strings.EqualFold(value, config.AwayTeam), teamLookupMatches(withoutSuffix, config.AwayTeam):
		return "away", config.AwayTeam
	default:
		return "", ""
	}
}

func inferTeamFromTranscript(transcript string, config matchstate.MatchConfig) (string, string) {
	normalized := normalizeLookup(transcript)
	if teamLookupMatches(normalized, config.HomeTeam) {
		return "home", config.HomeTeam
	}
	if teamLookupMatches(normalized, config.AwayTeam) {
		return "away", config.AwayTeam
	}
	return "", ""
}

func resolvePlayer(name string, config matchstate.MatchConfig) (string, string, string, bool) {
	lookup := normalizeLookup(name)
	if lookup == "" {
		return "", "", "", false
	}
	type candidate struct {
		teamID   string
		teamName string
		name     string
		score    int
	}
	best := candidate{}
	ambiguous := false
	consider := func(player matchstate.Player, teamID, teamName string) {
		playerName := strings.TrimSpace(player.Name)
		playerLookup := normalizeLookup(playerName)
		if playerLookup == "" {
			return
		}
		score := 0
		switch {
		case lookup == playerLookup:
			score = 1000
		case strings.Contains(lookup, playerLookup) || strings.Contains(playerLookup, lookup):
			shorter := len([]rune(lookup))
			if len([]rune(playerLookup)) < shorter {
				shorter = len([]rune(playerLookup))
			}
			if shorter < 2 {
				return
			}
			score = 100 + shorter
		default:
			return
		}
		if score > best.score {
			best = candidate{teamID: teamID, teamName: teamName, name: playerName, score: score}
			ambiguous = false
		} else if score == best.score && best.name != playerName {
			ambiguous = true
		}
	}
	for _, player := range config.HomePlayers {
		consider(player, "home", config.HomeTeam)
	}
	for _, player := range config.AwayPlayers {
		consider(player, "away", config.AwayTeam)
	}
	if best.score == 0 || ambiguous {
		return "", "", "", false
	}
	return best.teamID, best.teamName, best.name, true
}

func normalizeLookup(value string) string {
	var normalized strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			normalized.WriteRune(character)
		}
	}
	return normalized.String()
}

func teamLookupMatches(value, configured string) bool {
	configuredLookup := normalizeLookup(configured)
	configuredLookup = strings.TrimSuffix(strings.TrimSuffix(configuredLookup, "国家队"), "队")
	if value == "" || configuredLookup == "" {
		return false
	}
	return value == configuredLookup || strings.Contains(value, configuredLookup) || strings.Contains(configuredLookup, value)
}

func enrichExtraction(extraction Extraction, transcript string, config matchstate.MatchConfig) Extraction {
	extraction.EventType = normalizeEventType(extraction.EventType)
	if extraction.EventType == "" {
		extraction.EventType = normalizeEventType(inferEventType(transcript))
	}
	mentioned := mentionedPlayers(transcript, config)
	if len(extraction.Participants) == 0 {
		for index, name := range mentioned {
			role := inferredParticipantRole(extraction.EventType, index)
			if role == "" {
				break
			}
			extraction.Participants = append(extraction.Participants, ExtractedParticipant{Role: role, Name: name})
		}
	} else {
		seen := make(map[string]bool)
		for index := range extraction.Participants {
			if _, _, canonicalName, ok := resolvePlayer(extraction.Participants[index].Name, config); ok {
				extraction.Participants[index].Name = canonicalName
			}
			seen[normalizeLookup(extraction.Participants[index].Name)] = true
		}
		for _, name := range mentioned {
			_, _, canonicalName, resolved := resolvePlayer(name, config)
			if resolved {
				name = canonicalName
			}
			if seen[normalizeLookup(name)] {
				continue
			}
			role := inferredParticipantRole(extraction.EventType, len(extraction.Participants))
			if role == "" {
				break
			}
			extraction.Participants = append(extraction.Participants, ExtractedParticipant{Role: role, Name: name})
			seen[normalizeLookup(name)] = true
		}
	}
	if strings.TrimSpace(extraction.Team) == "" && len(extraction.Participants) > 0 {
		if teamID, teamName, _, ok := resolvePlayer(extraction.Participants[0].Name, config); ok {
			extraction.Team = teamName
			_ = teamID
		}
	}
	if strings.TrimSpace(extraction.Description) == "" && extraction.EventType != "" && len(mentioned) > 0 {
		extraction.Description = strings.TrimSpace(transcript)
	}
	return extraction
}

func inferEventType(transcript string) string {
	normalized := strings.ToLower(strings.TrimSpace(transcript))
	for _, match := range []struct {
		eventType string
		keywords  []string
	}{
		{"goal", []string{"进球", "球进了", "破门", "得分", "领先", "goal"}},
		{"substitution", []string{"换人", "上场", "下场", "substitution"}},
		{"red_card", []string{"红牌", "red card"}},
		{"yellow_card", []string{"黄牌", "yellow card"}},
		{"save", []string{"扑救", "save"}},
		{"foul", []string{"犯规", "foul"}},
		{"big_chance", []string{"绝佳机会", "单刀", "big chance"}},
		{"shot", []string{"射门", "起脚", "shot", "shoot"}},
	} {
		for _, keyword := range match.keywords {
			if strings.Contains(normalized, keyword) {
				return match.eventType
			}
		}
	}
	return ""
}

func inferredParticipantRole(eventType string, index int) string {
	roles := map[string][]string{
		"goal":         {"scorer", "assist", "pre_assist"},
		"shot":         {"shooter", "assist"},
		"big_chance":   {"attacker", "passer"},
		"save":         {"keeper", "shooter"},
		"miss":         {"shooter", "assist"},
		"foul":         {"offender", "fouled"},
		"yellow_card":  {"offender", "fouled"},
		"red_card":     {"offender", "fouled"},
		"substitution": {"sub_on", "sub_off"},
	}
	if index < 0 || index >= len(roles[eventType]) {
		return ""
	}
	return roles[eventType][index]
}

func mentionedPlayers(transcript string, config matchstate.MatchConfig) []string {
	normalizedTranscript := normalizeLookup(transcript)
	type mention struct {
		name  string
		index int
	}
	mentions := make([]mention, 0)
	consider := func(player matchstate.Player) {
		name := strings.TrimSpace(player.Name)
		lookup := normalizeLookup(name)
		if lookup == "" {
			return
		}
		index := strings.Index(normalizedTranscript, lookup)
		if index < 0 {
			for _, part := range strings.FieldsFunc(name, func(character rune) bool { return !unicode.IsLetter(character) && !unicode.IsDigit(character) }) {
				partLookup := normalizeLookup(part)
				if len([]rune(partLookup)) < 2 {
					continue
				}
				if candidateIndex := strings.Index(normalizedTranscript, partLookup); candidateIndex >= 0 && (index < 0 || candidateIndex < index) {
					index = candidateIndex
				}
			}
		}
		if index >= 0 {
			mentions = append(mentions, mention{name: name, index: index})
		}
	}
	for _, player := range config.HomePlayers {
		consider(player)
	}
	for _, player := range config.AwayPlayers {
		consider(player)
	}
	for left := 0; left < len(mentions); left++ {
		for right := left + 1; right < len(mentions); right++ {
			if mentions[right].index < mentions[left].index {
				mentions[left], mentions[right] = mentions[right], mentions[left]
			}
		}
	}
	result := make([]string, 0, len(mentions))
	seen := make(map[string]bool)
	for _, item := range mentions {
		lookup := normalizeLookup(item.name)
		if seen[lookup] {
			continue
		}
		seen[lookup] = true
		result = append(result, item.name)
	}
	return result
}

func normalizeEventType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	if allowedEventTypes[value] {
		return value
	}
	aliases := map[string]string{
		"进球": "goal", "破门": "goal", "得分": "goal", "射门": "shot", "起脚": "shot",
		"绝佳机会": "big_chance", "扑救": "save", "错失": "miss", "犯规": "foul",
		"黄牌": "yellow_card", "红牌": "red_card", "点球": "penalty", "换人": "substitution",
		"伤停": "injury", "持续压迫": "pressure", "战术变化": "tactical_shift",
		"var检查": "var_check", "var结果": "var_result",
	}
	return aliases[normalizeLookup(value)]
}

func normalizeParticipantRole(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	aliases := map[string]string{
		"进球者": "scorer", "得分者": "scorer", "射门者": "shooter", "进攻者": "attacker",
		"传球者": "passer", "助攻者": "assist", "策动者": "pre_assist", "门将": "keeper", "扑救门将": "keeper",
		"防守者": "defender", "封堵者": "blocker", "犯规者": "offender", "被犯规者": "fouled", "对抗对象": "fouled",
		"主罚者": "taker", "造点者": "won_by", "上场": "sub_on", "下场": "sub_off", "受伤球员": "injured", "对抗球员": "challenger",
	}
	if role := aliases[normalizeLookup(value)]; role != "" {
		return role
	}
	return value
}

var opponentRoles = map[string]map[string]bool{
	"goal": {"defender": true}, "shot": {"blocker": true, "keeper": true}, "big_chance": {"defender": true, "keeper": true},
	"save": {"shooter": true}, "miss": {"keeper": true}, "foul": {"fouled": true},
	"yellow_card": {"fouled": true}, "red_card": {"fouled": true}, "penalty": {"offender": true, "keeper": true},
	"pressure": {"target": true}, "injury": {"challenger": true},
}

func expectedParticipantTeam(eventType, role, eventTeamID string) string {
	if eventTeamID != "home" && eventTeamID != "away" {
		return eventTeamID
	}
	if opponentRoles[eventType][role] {
		if eventTeamID == "home" {
			return "away"
		}
		return "home"
	}
	return eventTeamID
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

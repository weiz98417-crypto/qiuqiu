package companion

import (
	"fmt"
	"strconv"
	"strings"

	"qiuqiu/internal/matchstate"
)

func assessMatchClaim(text string, snapshot matchstate.Snapshot, events []matchstate.MatchEvent) (FactClaim, []string, bool) {
	if isScoreClaim(text) {
		claim, ok := assessScoreClaim(text, snapshot)
		if issue := snapshotIntegrityIssue(snapshot); ok && issue != "" {
			claim.Status = ClaimStatusUnverified
			claim.Reason = issue
		}
		return claim, nil, ok
	}
	if !isEventClaim(text) {
		return FactClaim{}, nil, false
	}
	claimedPlayer := inferClaimedPlayer(text, snapshot)
	claimedTeam := inferClaimedTeam(text, snapshot)
	claim := FactClaim{
		Kind:          "event",
		EventType:     "goal",
		Certainty:     claimCertainty(text),
		ClaimedPlayer: claimedPlayer,
		ClaimedTeam:   claimedTeam,
		Status:        ClaimStatusUnverified,
	}
	if issue := snapshotIntegrityIssue(snapshot); issue != "" {
		claim.Reason = issue
		return claim, nil, true
	}
	for _, event := range events {
		if event.EventType != "goal" {
			continue
		}
		actualPlayer := strings.TrimSpace(event.PlayerName)
		if scorers := participantNames(event.Participants, "scorer"); len(scorers) > 0 {
			actualPlayer = scorers[0]
		}
		claim.ActualPlayer = actualPlayer
		claim.ActualTeam = strings.TrimSpace(event.TeamName)
		if claim.ActualTeam == "" {
			switch event.TeamID {
			case "home":
				claim.ActualTeam = snapshot.HomeTeam
			case "away":
				claim.ActualTeam = snapshot.AwayTeam
			}
		}
		if claimedPlayer != "" && strings.EqualFold(claimedPlayer, actualPlayer) {
			claim.Status = ClaimStatusConfirmed
		} else if claimedPlayer != "" && actualPlayer != "" {
			claim.Status = ClaimStatusContradicted
		} else if claimedTeam != "" && strings.EqualFold(claimedTeam, claim.ActualTeam) {
			claim.Status = ClaimStatusConfirmed
		} else if claimedTeam != "" && claim.ActualTeam != "" {
			claim.Status = ClaimStatusContradicted
		}
		return claim, []string{event.ID}, true
	}
	return claim, nil, true
}

func inferClaimedTeam(text string, snapshot matchstate.Snapshot) string {
	if snapshot.HomeTeam != "" && strings.Contains(text, snapshot.HomeTeam) {
		return snapshot.HomeTeam
	}
	if snapshot.AwayTeam != "" && strings.Contains(text, snapshot.AwayTeam) {
		return snapshot.AwayTeam
	}
	return ""
}

func inferClaimedPlayer(text string, snapshot matchstate.Snapshot) string {
	if known := inferPlayer(text); known != "" {
		return known
	}
	prefix := text
	for _, marker := range []string{"进球了", "破门了", "得分了", "进啦", "进咯", "进喽", "球进了", "进了"} {
		if index := strings.Index(prefix, marker); index >= 0 {
			prefix = prefix[:index]
			break
		}
	}
	prefix = strings.ReplaceAll(prefix, snapshot.HomeTeam, "")
	prefix = strings.ReplaceAll(prefix, snapshot.AwayTeam, "")
	prefix = strings.Trim(prefix, " \t\r\n，。！？!?：:、的")
	for _, lead := range []string{"刚刚", "刚才", "好像", "似乎", "可能", "应该", "大概", "听说", "我看", "我觉得", "这球", "那个球"} {
		prefix = strings.TrimPrefix(prefix, lead)
		prefix = strings.Trim(prefix, " \t\r\n，。！？!?：:、的")
	}
	// intent-router C2: insistence adverbs and bare connective particles are
	// not player names — "确实进了"/"真的又进了" must read as a player-less
	// insistence, or the claim would "contradict" a name like 确实进了.
	for {
		stripped := prefix
		for _, filler := range insistenceAdverbs {
			stripped = strings.TrimPrefix(strings.TrimSuffix(stripped, filler), filler)
		}
		for _, particle := range []string{"又", "也", "再", "才", "就", "都"} {
			stripped = strings.TrimSuffix(strings.TrimPrefix(stripped, particle), particle)
		}
		stripped = strings.Trim(stripped, " \t\r\n，。！？!?：:、的")
		if stripped == prefix {
			break
		}
		prefix = stripped
	}
	if fields := strings.Fields(prefix); len(fields) > 0 {
		prefix = fields[len(fields)-1]
	}
	if prefix == "" || containsAny(prefix, "主队", "客队", "他们", "他", "她", "有人") {
		return ""
	}
	runes := []rune(prefix)
	if len(runes) > 24 {
		return ""
	}
	return prefix
}

func snapshotIntegrityIssue(snapshot matchstate.Snapshot) string {
	if snapshot.Integrity.Status == "conflict" {
		if snapshot.Integrity.Reason != "" {
			return snapshot.Integrity.Reason
		}
		return "match sources conflict"
	}
	if snapshot.Score.Home < 0 || snapshot.Score.Away < 0 {
		return "match score is invalid"
	}
	if len(snapshot.RecentEvents) == 0 {
		return ""
	}
	if snapshot.RecentEvents[0].Score != snapshot.Score {
		return "latest event score does not match snapshot"
	}
	events := snapshot.RecentEvents
	oldest := events[len(events)-1]
	current := oldest.Score
	if oldest.EventType == "goal" {
		if oldest.Period == "pre_match" {
			return "goal occurred before kickoff"
		}
		switch oldest.TeamID {
		case "home":
			current.Home--
		case "away":
			current.Away--
		default:
			return "goal has no valid team"
		}
		if current.Home < 0 || current.Away < 0 {
			return "goal did not increase score"
		}
	}
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		expected := current
		switch event.EventType {
		case "goal":
			if event.Period == "pre_match" {
				return "goal occurred before kickoff"
			}
			switch event.TeamID {
			case "home":
				expected.Home++
			case "away":
				expected.Away++
			default:
				return "goal has no valid team"
			}
			if event.Score != expected {
				return "goal score transition is inconsistent"
			}
		case "var_check":
			if scoreDistance(current, event.Score) > 1 {
				return "VAR score transition is inconsistent"
			}
		default:
			if event.Score != expected {
				return "non-goal event changed score"
			}
		}
		current = event.Score
	}
	return ""
}

func scoreDistance(left, right matchstate.Score) int {
	home := left.Home - right.Home
	if home < 0 {
		home = -home
	}
	away := left.Away - right.Away
	if away < 0 {
		away = -away
	}
	return home + away
}

func assessScoreClaim(text string, snapshot matchstate.Snapshot) (FactClaim, bool) {
	parts := scoreClaimPattern.FindStringSubmatch(text)
	if len(parts) != 3 {
		return FactClaim{}, false
	}
	left, leftErr := strconv.Atoi(parts[1])
	right, rightErr := strconv.Atoi(parts[2])
	if leftErr != nil || rightErr != nil {
		return FactClaim{}, false
	}
	claimed := matchstate.Score{Home: left, Away: right}
	mentionsAway := snapshot.AwayTeam != "" && strings.Contains(text, snapshot.AwayTeam)
	mentionsHome := snapshot.HomeTeam != "" && strings.Contains(text, snapshot.HomeTeam)
	scoreRange := scoreClaimPattern.FindStringIndex(text)
	awayBeforeScore := len(scoreRange) == 2 && snapshot.AwayTeam != "" && strings.Contains(text[:scoreRange[0]], snapshot.AwayTeam)
	homeAfterScore := len(scoreRange) == 2 && snapshot.HomeTeam != "" && strings.Contains(text[scoreRange[1]:], snapshot.HomeTeam)
	if (awayBeforeScore && homeAfterScore) || (mentionsAway && !mentionsHome) {
		claimed = matchstate.Score{Home: right, Away: left}
	}
	claim := FactClaim{
		Kind:         "score",
		Certainty:    claimCertainty(text),
		ClaimedScore: &claimed,
		ActualScore:  &snapshot.Score,
		Status:       ClaimStatusContradicted,
	}
	if claimed == snapshot.Score {
		claim.Status = ClaimStatusConfirmed
	}
	return claim, true
}

func claimCertainty(text string) string {
	if containsAny(text, "好像", "似乎", "可能", "应该", "大概", "听说", "吧") {
		return "uncertain"
	}
	return "asserted"
}

func answerRecentEvent(text string, events []matchstate.MatchEvent) (string, []string) {
	if isRecentGoalScorerQuestion(text) {
		for _, ev := range events {
			if ev.EventType != "goal" {
				continue
			}
			scorers := participantNames(ev.Participants, "scorer")
			if len(scorers) == 0 && strings.TrimSpace(ev.PlayerName) != "" {
				scorers = []string{strings.TrimSpace(ev.PlayerName)}
			}
			if len(scorers) == 0 {
				return fmt.Sprintf("刚才%s有进球，但进球球员还没有确认。", ev.Clock), []string{ev.ID}
			}
			return fmt.Sprintf("刚才%s这球是%s打进的。", ev.Clock, strings.Join(scorers, "、")), []string{ev.ID}
		}
		return "我这边目前还没有收到进球记录。", nil
	}
	if containsAny(text, "助攻", "谁主攻") {
		for _, ev := range events {
			if ev.EventType != "goal" {
				continue
			}
			assist := participantNames(ev.Participants, "assist")
			preAssist := participantNames(ev.Participants, "pre_assist")
			var parts []string
			if len(assist) > 0 {
				parts = append(parts, strings.Join(assist, "、")+"助攻")
			}
			if len(preAssist) > 0 {
				parts = append(parts, strings.Join(preAssist, "、")+"参与策动")
			}
			if len(parts) == 0 {
				return "我这边只看到刚才有进球记录，但没有看到明确助攻人。", []string{ev.ID}
			}
			return "刚才这球是" + strings.Join(parts, "，") + "。", []string{ev.ID}
		}
		return "我这边目前还没看到进球助攻记录。", nil
	}
	if len(events) == 0 {
		return "我这边目前还没有收到新的比赛动态。", nil
	}
	ev := events[0]
	return fmt.Sprintf("刚才是%s %s：%s", ev.Clock, eventLabel(ev.EventType), ev.Description), []string{ev.ID}
}

func answerFollowUp(text string, turns []ConversationTurn, events []matchstate.MatchEvent) (string, []string) {
	event := followUpEvent(turns, events)
	if event == nil {
		return "这个追问我需要基于前面那条事件来答，但我这边暂时没有可用的上下文记录。", nil
	}
	if containsAny(text, "策动", "谁参与") {
		preAssist := participantNames(event.Participants, "pre_assist")
		if len(preAssist) == 0 {
			return "这球我只看到进球或助攻记录，暂时没有明确策动者。", []string{event.ID}
		}
		return "这球策动的是" + strings.Join(preAssist, "、") + "。", []string{event.ID}
	}
	if containsAny(text, "传", "传的") {
		assist := participantNames(event.Participants, "assist")
		if len(assist) == 0 {
			return "这球我这边暂时没有明确传球助攻记录。", []string{event.ID}
		}
		return "最后一传是" + strings.Join(assist, "、") + "。", []string{event.ID}
	}
	return answerRecentEvent(text, []matchstate.MatchEvent{*event})
}

func followUpEvent(turns []ConversationTurn, events []matchstate.MatchEvent) *matchstate.MatchEvent {
	referenced := lastReferencedEventID(turns)
	if referenced != "" {
		for i := range events {
			if events[i].ID == referenced {
				return &events[i]
			}
		}
	}
	for i := range events {
		if events[i].EventType == "goal" {
			return &events[i]
		}
	}
	if len(events) == 0 {
		return nil
	}
	return &events[0]
}

func lastReferencedEventID(turns []ConversationTurn) string {
	for i := len(turns) - 1; i >= 0; i-- {
		if strings.TrimSpace(turns[i].EventID) != "" {
			return turns[i].EventID
		}
	}
	return ""
}

func answerPlayerQuestion(text, player string, events []matchstate.MatchEvent) (string, []string) {
	if player == "" {
		return "你说的是哪位球员？我帮你翻一下刚才的比赛记录。", nil
	}
	var ids []string
	for _, ev := range events {
		ids = append(ids, ev.ID)
		if ev.EventType == "goal" && containsAny(text, "进球") {
			return fmt.Sprintf("有，我这边看到%s在%s有进球记录：%s。", player, ev.Clock, ev.Description), ids
		}
	}
	if containsAny(text, "进球") {
		return fmt.Sprintf("我这边目前没有看到%s的进球记录。", player), ids
	}
	if len(events) == 0 {
		return fmt.Sprintf("我这边目前还没有%s的实时事件记录。", player), nil
	}
	return fmt.Sprintf("我这边看到%s最近参与了%d条事件，最新一条是：%s。", player, len(events), events[0].Description), ids
}

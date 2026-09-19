package companion

import (
	"fmt"
	"strings"
	"time"

	"qiuqiu/internal/matchstate"
)

func BuildScheduleSearchRequest(intent ScheduleIntent, now time.Time, timezone string) ScheduleSearchRequest {
	location := scheduleLocation(now, timezone)
	if now.IsZero() {
		now = time.Now()
	}
	localNow := now.In(location)
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	switch intent.Scope {
	case ScheduleScopeTomorrow:
		start = start.AddDate(0, 0, 1)
	case ScheduleScopeNearby:
		return ScheduleSearchRequest{
			From:        start,
			To:          start.AddDate(0, 0, 2),
			Timezone:    location.String(),
			Competition: intent.Competition,
		}
	}
	return ScheduleSearchRequest{
		From:        start,
		To:          start.AddDate(0, 0, 1),
		Timezone:    location.String(),
		Competition: intent.Competition,
	}
}

func scheduleLocation(now time.Time, timezone string) *time.Location {
	if name := strings.TrimSpace(timezone); name != "" {
		if location, err := time.LoadLocation(name); err == nil {
			return location
		}
		normalized := strings.ToUpper(name)
		if strings.HasPrefix(normalized, "UTC") {
			if parsed, err := time.Parse("Z07:00", strings.TrimPrefix(normalized, "UTC")); err == nil {
				_, offsetSeconds := parsed.Zone()
				return time.FixedZone(normalized, offsetSeconds)
			}
		}
	}
	if now.Location() != nil {
		return now.Location()
	}
	return time.UTC
}

func scheduleSearchToolArgs(request ScheduleSearchRequest) map[string]string {
	return map[string]string{
		"from":     request.From.Format(time.RFC3339),
		"to":       request.To.Format(time.RFC3339),
		"timezone": request.Timezone,
	}
}

func formatTodaySchedule(fixtures []ScheduleMatch) string {
	if len(fixtures) == 0 {
		return "我这边查到今天暂时没有赛程。"
	}
	pairs := make([]string, 0, minInt(len(fixtures), 5))
	for _, fixture := range fixtures {
		home := strings.TrimSpace(fixture.HomeTeam)
		away := strings.TrimSpace(fixture.AwayTeam)
		if home == "" || away == "" {
			continue
		}
		pairs = append(pairs, home+"对"+away)
		if len(pairs) >= 5 {
			break
		}
	}
	if len(pairs) == 0 {
		return "我这边查到今天暂时没有赛程。"
	}
	return "今天有：" + strings.Join(pairs, "、") + "。"
}

func formatScheduleSearchResult(result ScheduleSearchResult, scope ScheduleScope) string {
	if scheduleFreshnessUnreliable(result.Freshness) {
		return "赛程数据现在有延迟或冲突，我先不拿它当准确信息报给你。"
	}
	if len(result.Fixtures) == 0 {
		if scope == ScheduleScopeNearby {
			return "我查到今天和明天暂时没有可靠的赛程。"
		}
		return "我查到这个时间段暂时没有可靠的赛程。"
	}
	pairs := make([]string, 0, minInt(len(result.Fixtures), 5))
	for _, fixture := range result.Fixtures {
		if scheduleFreshnessUnreliable(fixture.Freshness) {
			continue
		}
		home := strings.TrimSpace(fixture.HomeTeam)
		away := strings.TrimSpace(fixture.AwayTeam)
		if home == "" || away == "" {
			continue
		}
		label := home + "对" + away
		details := make([]string, 0, 3)
		if competition := strings.TrimSpace(fixture.Competition); competition != "" {
			details = append(details, competition)
		}
		if !fixture.KickoffAt.IsZero() {
			details = append(details, fixture.KickoffAt.Format("15:04")+"开球")
		}
		if status := scheduleStatusLabel(fixture.Status); status != "" {
			details = append(details, status)
		}
		if len(details) > 0 {
			label += "（" + strings.Join(details, "，") + "）"
		}
		if fixture.HomeScore != nil && fixture.AwayScore != nil {
			label += fmt.Sprintf(" %d-%d", *fixture.HomeScore, *fixture.AwayScore)
		}
		pairs = append(pairs, label)
		if len(pairs) >= 5 {
			break
		}
	}
	if len(pairs) == 0 {
		return "我查到这个时间段暂时没有可靠的赛程。"
	}
	label := "这个时间段"
	switch scope {
	case ScheduleScopeToday:
		label = "今天"
	case ScheduleScopeTomorrow:
		label = "明天"
	case ScheduleScopeNearby:
		label = "今天和明天"
	}
	reply := label + "有：" + strings.Join(pairs, "、") + "。"
	source := strings.TrimSpace(result.Source)
	if source == "" {
		for _, fixture := range result.Fixtures {
			if source = strings.TrimSpace(fixture.Source); source != "" {
				break
			}
		}
	}
	metadata := make([]string, 0, 2)
	if source != "" {
		metadata = append(metadata, "来源："+source)
	}
	if freshness := scheduleFreshnessLabel(result.Freshness); freshness != "" {
		metadata = append(metadata, freshness)
	}
	if len(metadata) > 0 {
		reply += strings.Join(metadata, "，") + "。"
	}
	return reply
}

func scheduleFreshnessUnreliable(freshness string) bool {
	switch strings.ToLower(strings.TrimSpace(freshness)) {
	case "stale", "conflict", "conflicted", "unreliable", "error":
		return true
	default:
		return false
	}
}

func scheduleFreshnessLabel(freshness string) string {
	switch strings.ToLower(strings.TrimSpace(freshness)) {
	case "fresh":
		return "数据刚刚更新"
	case "cached":
		return "缓存数据"
	default:
		return ""
	}
}

func scheduleStatusLabel(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "", "NS", "TBD", "SCHEDULED":
		return ""
	case "FT", "AET", "PEN", "FINISHED":
		return "已结束"
	case "CANC", "PST", "CANCELED", "POSTPONED":
		return "已调整"
	default:
		return "进行中"
	}
}

func activeMatchScheduleReply(snapshot matchstate.Snapshot) (string, bool) {
	integrity := strings.ToLower(strings.TrimSpace(snapshot.Integrity.Status))
	if integrity != "" && integrity != "ok" {
		return "", false
	}
	homeTeam := strings.TrimSpace(snapshot.HomeTeam)
	awayTeam := strings.TrimSpace(snapshot.AwayTeam)
	if homeTeam == "" || awayTeam == "" {
		return "", false
	}
	period := strings.TrimSpace(snapshot.Period)
	if period == "" {
		period = strings.TrimSpace(snapshot.MatchClock.Period)
	}
	switch strings.ToLower(period) {
	case "", "pre_match", "fulltime", "full_time", "finished":
		return "", false
	}
	matchLabel := homeTeam + "对" + awayTeam
	if competition := strings.TrimSpace(snapshot.Competition); competition != "" {
		matchLabel += "的" + competition
	}
	return fmt.Sprintf("现在正在看%s，%s %d-%d %s，时间在%s %s。",
		matchLabel,
		homeTeam,
		snapshot.Score.Home,
		snapshot.Score.Away,
		awayTeam,
		displayPeriod(period),
		snapshot.Clock,
	), true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

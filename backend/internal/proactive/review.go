package proactive

// 赛后复盘邀约（openspec/changes/proactive-match-nodes）：终场后 15 分钟
// 的复盘话轮提醒。与赛前提醒共用同一簿子、同一 outbox 补递与过期语义，
// 只是时序推导不同——DeliverAt=终场+15min，ExpireAt=终场+2h（隔天的复盘
// 邀约是打扰不是陪伴）。
import "time"

const (
	// KindPreMatch 是默认空值：用户显式请求的赛前提醒。
	KindPreMatch = ""
	// KindFulltimeReview 是终场复盘邀约。
	KindFulltimeReview = "fulltime_review"

	ReviewDeliverAfter = 15 * time.Minute
	ReviewExpireAfter  = 2 * time.Hour
)

// FulltimeReviewReply 是复盘邀约的确定性文本：对阵事实由提醒簿拥有，
// 不 LLM 化（与 PreMatchReminderReply 同纪律）。
func FulltimeReviewReply(r Reminder) string {
	return "刚看完这场 " + r.HomeTeam + " 对 " + r.AwayTeam + "，回来聊聊？我还记着呢。"
}

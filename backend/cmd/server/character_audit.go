package main

// character_audit（openspec/changes/polish-round-3 T1）：人格互动规范变更
// 的入口层审计。Ledger 在 watchDeps / character_api 两侧注入，本 helper
// 统一事件形状（Kind=character_setting，InputText 记 field/from/to/source）。

import (
	"context"
	"log"
	"strconv"
	"time"

	"qiuqiu/internal/interaction"
	"qiuqiu/internal/relationship"
)

// recordCharacterChange 落一条 character_setting 账本事件；Ledger 缺席或
// 失败只记日志——审计尽力而为，不阻塞设置本身。
func recordCharacterChange(ctx context.Context, ledger interaction.Ledger, characterSettings *relationship.CharacterSettings, userID, field, value, source string) {
	if ledger == nil {
		return
	}
	from := ""
	if characterSettings != nil {
		if values, err := characterSettings.Get(ctx, userID); err == nil {
			from = values[relationship.CharacterSettingField(field)]
		}
	}
	event := interaction.Event{
		ID:      "character-" + userID + "-" + source + "-" + strconv.FormatInt(time.Now().UnixMilli(), 10),
		Kind:    interaction.KindCharacterSetting,
		UserID:  userID,
		// MatchID 校验必填，但设置是用户级事件：用 "account" 哨兵标注
		// 无比赛上下文。
		MatchID:   "account",
		InputText: "field=" + field + " from=" + from + " to=" + value,
		Source:    source,
		CreatedAt: time.Now().UTC(),
	}
	if _, err := ledger.Append(ctx, event); err != nil {
		log.Printf("character settings audit for %q: %v", userID, err)
	}
}

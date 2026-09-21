package relationship

// 人格互动规范设置（openspec/changes/character-settings）：三入口一状态
// 的「状态」——cue 词、WS set_character、HTTP /api/me/character 共同读写
// 同一份按用户持久化的互动规范。不变式（CONTEXT.md 角色立场边界）：这里
// 只存互动规范，永不存角色立场/价值观/身份；ProfanityEnabled 不上用户面。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CharacterSettingField 是可由用户调节的互动规范槽位。
type CharacterSettingField string

const (
	SettingInitiative       CharacterSettingField = "initiative"
	SettingAnalysisAppetite CharacterSettingField = "analysis_appetite"
	SettingBanterLevel      CharacterSettingField = "banter_level"
)

// 合法值白名单：cue 词 / WS / HTTP 三个入口共用同一校验。
var characterSettingValues = map[CharacterSettingField]map[string]bool{
	SettingInitiative:       {"quiet": true, "normal": true, "active": true},
	SettingAnalysisAppetite: {"brief": true, "standard": true, "deep": true},
	SettingBanterLevel:      {"off": true, "light": true, "playful": true},
}

// ValidateCharacterSetting 报告某槽位的取值是否合法。
func ValidateCharacterSetting(field, value string) bool {
	allowed, ok := characterSettingValues[CharacterSettingField(field)]
	if !ok {
		return false
	}
	return allowed[strings.TrimSpace(value)]
}

// CharacterSettingStore 是互动规范的持久化接缝（Postgres 生产实现 +
// 内存测试实现，migration 048）。
type CharacterSettingStore interface {
	Get(ctx context.Context, userID string) (map[CharacterSettingField]string, error)
	Set(ctx context.Context, userID string, field CharacterSettingField, value string) error
}

// CharacterSettings 是三入口共享的读改服务。
type CharacterSettings struct {
	store CharacterSettingStore
}

func NewCharacterSettings(store CharacterSettingStore) (*CharacterSettings, error) {
	if store == nil {
		return nil, fmt.Errorf("character settings store unavailable")
	}
	return &CharacterSettings{store: store}, nil
}

// Set 校验并落一个槽位；返回落库后的完整设置（供 ack / 运营台展示）。
func (s *CharacterSettings) Set(ctx context.Context, userID, field, value string) (map[CharacterSettingField]string, error) {
	fieldName := CharacterSettingField(strings.TrimSpace(field))
	value = strings.TrimSpace(value)
	if !ValidateCharacterSetting(string(fieldName), value) {
		return nil, fmt.Errorf("invalid character setting %s=%q", field, value)
	}
	if err := s.store.Set(ctx, userID, fieldName, value); err != nil {
		return nil, err
	}
	return s.store.Get(ctx, userID)
}

// Get 返回完整设置（缺失槽位为空串 = 未自定义）。
func (s *CharacterSettings) Get(ctx context.Context, userID string) (map[CharacterSettingField]string, error) {
	return s.store.Get(ctx, userID)
}

// PostgresCharacterSettingStore 是生产实现（migration 048）。
type PostgresCharacterSettingStore struct {
	pool *pgxpool.Pool
}

func OpenPostgresCharacterSettingStore(ctx context.Context, databaseURL string) (*PostgresCharacterSettingStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("character settings connect: %w", err)
	}
	return &PostgresCharacterSettingStore{pool: pool}, nil
}

// Close 释放连接池。
func (s *PostgresCharacterSettingStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresCharacterSettingStore) Set(ctx context.Context, userID string, field CharacterSettingField, value string) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO user_character_settings (user_id, initiative, analysis_appetite, banter_level, updated_at)
VALUES ($1,
        CASE WHEN $2 = 'initiative' THEN $3 ELSE '' END,
        CASE WHEN $2 = 'analysis_appetite' THEN $3 ELSE '' END,
        CASE WHEN $2 = 'banter_level' THEN $3 ELSE '' END,
        now())
ON CONFLICT (user_id) DO UPDATE SET
  initiative = CASE WHEN $2 = 'initiative' THEN $3 ELSE user_character_settings.initiative END,
  analysis_appetite = CASE WHEN $2 = 'analysis_appetite' THEN $3 ELSE user_character_settings.analysis_appetite END,
  banter_level = CASE WHEN $2 = 'banter_level' THEN $3 ELSE user_character_settings.banter_level END,
  updated_at = now()`, userID, string(field), value)
	if err != nil {
		return fmt.Errorf("character settings set: %w", err)
	}
	return nil
}

func (s *PostgresCharacterSettingStore) Get(ctx context.Context, userID string) (map[CharacterSettingField]string, error) {
	row := s.pool.QueryRow(ctx, `
SELECT initiative, analysis_appetite, banter_level FROM user_character_settings WHERE user_id = $1`, userID)
	settings := map[CharacterSettingField]string{
		SettingInitiative:       "",
		SettingAnalysisAppetite: "",
		SettingBanterLevel:      "",
	}
	var initiative, appetite, banter string
	if err := row.Scan(&initiative, &appetite, &banter); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return settings, nil
		}
		return nil, fmt.Errorf("character settings get: %w", err)
	}
	settings[SettingInitiative] = initiative
	settings[SettingAnalysisAppetite] = appetite
	settings[SettingBanterLevel] = banter
	return settings, nil
}

// MemoryCharacterSettingStore 是测试/无库降级实现。
type MemoryCharacterSettingStore struct {
	settings map[string]map[CharacterSettingField]string
}

func NewMemoryCharacterSettingStore() *MemoryCharacterSettingStore {
	return &MemoryCharacterSettingStore{settings: map[string]map[CharacterSettingField]string{}}
}

func (s *MemoryCharacterSettingStore) Get(_ context.Context, userID string) (map[CharacterSettingField]string, error) {
	if s == nil {
		return nil, fmt.Errorf("character settings store unavailable")
	}
	out := map[CharacterSettingField]string{
		SettingInitiative:       "",
		SettingAnalysisAppetite: "",
		SettingBanterLevel:      "",
	}
	for field, value := range s.settings[userID] {
		out[field] = value
	}
	return out, nil
}

func (s *MemoryCharacterSettingStore) Set(_ context.Context, userID string, field CharacterSettingField, value string) error {
	if s == nil {
		return fmt.Errorf("character settings store unavailable")
	}
	if s.settings[userID] == nil {
		s.settings[userID] = map[CharacterSettingField]string{}
	}
	s.settings[userID][field] = value
	return nil
}

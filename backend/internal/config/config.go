package config

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"qiuqiu/internal/router"
)

type Config struct {
	Port                           string
	Environment                    string
	AppToken                       string
	SecondaryAppToken              string
	AuthMode                       string
	SessionSigningKey              string
	AllowedOrigins                 []string
	DatabaseURL                    string
	RedisAddr                      string
	MiMoAPIKey                     string
	MiMoBaseURL                    string
	MiMoModel                      string
	MiMoVoice                      string
	CompanionRealizerTimeoutMS     int
	RouterAPIKey                   string
	RouterBaseURL                  string
	RouterModel                    string
	RouterTimeoutMS                int
	APISportsAPIKey                string
	APISportsBaseURL               string
	// 语义记忆（openspec/changes/semantic-memory）：端点留空即整体停用
	//（行为=现状 contains 单路）；本地 Ollama 形态见 deploy/.env.example。
	EmbeddingBaseURL               string
	EmbeddingModel                 string
	PrivacyRetentionDays           int
	PendingObservationCoordination bool
	FactLedgerPublicReads          bool
	MemobaseURL                    string
	MemobaseToken                  string
	MemobaseExtractionTimeoutMS    int
	// ADR-0010: HS256 secret for the console human auth channel. Required
	// once any operator password account exists (enforced at login).
	JWTSecret string
}

func Load() *Config {
	environment := getEnv("APP_ENV", "development")
	authMode := strings.TrimSpace(os.Getenv("AUTH_MODE"))
	if authMode == "" {
		if strings.EqualFold(environment, "production") {
			authMode = "session"
		} else {
			authMode = "dual"
		}
	}
	sessionSigningKey := strings.TrimSpace(os.Getenv("SESSION_SIGNING_KEY"))
	if sessionSigningKey == "" && !strings.EqualFold(environment, "production") {
		sessionSigningKey = developmentSessionSigningKey()
	}
	// ADR-0009 intent router: the ROUTER_* env contract is owned by the
	// router package (ROUTER_API_KEY falls back to MIMO_API_KEY; both unset
	// keeps the layer disabled entirely — CI/evals behaviour is unchanged).
	routerConfig := router.NewConfig(os.Getenv)
	return &Config{
		Port:                           getEnv("PORT", "8080"),
		Environment:                    environment,
		AppToken:                       strings.TrimSpace(os.Getenv("APP_TOKEN")),
		SecondaryAppToken:              strings.TrimSpace(os.Getenv("APP_TOKEN_SECONDARY")),
		AuthMode:                       authMode,
		SessionSigningKey:              sessionSigningKey,
		AllowedOrigins:                 splitCSV(os.Getenv("ALLOWED_ORIGINS")),
		DatabaseURL:                    getEnv("DATABASE_URL", ""),
		RedisAddr:                      getEnv("REDIS_ADDR", "localhost:6379"),
		MiMoAPIKey:                     getEnv("MIMO_API_KEY", ""),
		MiMoBaseURL:                    getEnv("MIMO_BASE_URL", "https://api.xiaomimimo.com/v1"),
		MiMoModel:                      getEnv("MIMO_MODEL", "mimo-v2.5-pro"),
		EmbeddingBaseURL:               strings.TrimSpace(getEnv("EMBEDDING_BASE_URL", "")),
		EmbeddingModel:                 getEnv("EMBEDDING_MODEL", "bge-m3"),
		MiMoVoice:                      getEnv("MIMO_VOICE", "冰糖"),
		CompanionRealizerTimeoutMS:     getEnvInt("COMPANION_REALIZER_TIMEOUT_MS", 5000),
		RouterAPIKey:                   routerConfig.APIKey,
		RouterBaseURL:                  routerConfig.BaseURL,
		RouterModel:                    routerConfig.Model,
		RouterTimeoutMS:                int(routerConfig.Timeout / time.Millisecond),
		APISportsAPIKey:                strings.TrimSpace(os.Getenv("APISPORTS_API_KEY")),
		APISportsBaseURL:               getEnv("APISPORTS_BASE_URL", "https://v3.football.api-sports.io"),
		PrivacyRetentionDays:           getEnvInt("PRIVACY_RETENTION_DAYS", 30),
		PendingObservationCoordination: getEnvBool("PENDING_OBSERVATION_COORDINATION", true),
		FactLedgerPublicReads:          getEnvBool("FACT_LEDGER_PUBLIC_READS", true),
		MemobaseURL:                    getEnv("MEMOBASE_URL", "http://localhost:8019"),
		MemobaseToken:                  strings.TrimSpace(os.Getenv("MEMOBASE_TOKEN")),
		MemobaseExtractionTimeoutMS:    getEnvInt("MEMOBASE_EXTRACTION_TIMEOUT_MS", 10000),
		JWTSecret:                      strings.TrimSpace(os.Getenv("QIUQIU_JWT_SECRET")),
	}
}

func (c *Config) RedisEnabled() bool {
	return c.RedisAddr != ""
}

func (c *Config) CompanionRealizerTimeout() time.Duration {
	return time.Duration(c.CompanionRealizerTimeoutMS) * time.Millisecond
}

func (c *Config) MemobaseExtractionTimeout() time.Duration {
	return time.Duration(c.MemobaseExtractionTimeoutMS) * time.Millisecond
}

func (c *Config) WSReadLimit() int64 {
	return 1 << 20 // 1MB: enough for a 15-second 16kHz mono voice turn.
}

func (c *Config) WSWriteTimeout() int {
	return 10 // seconds
}

func (c *Config) WSReadTimeout() int {
	return 60 // seconds
}

func (c *Config) MaxConnsPerIP() int {
	return 5
}

func (c *Config) Validate() error {
	if c.CompanionRealizerTimeoutMS == 0 {
		c.CompanionRealizerTimeoutMS = 5000
	}
	if c.CompanionRealizerTimeoutMS < 0 || c.CompanionRealizerTimeoutMS > 10000 {
		return fmt.Errorf("COMPANION_REALIZER_TIMEOUT_MS must be between 1 and 10000")
	}
	if c.PrivacyRetentionDays == 0 {
		c.PrivacyRetentionDays = 30
	}
	if c.PrivacyRetentionDays < 0 {
		return fmt.Errorf("PRIVACY_RETENTION_DAYS must be greater than zero")
	}
	if strings.EqualFold(c.Environment, "production") {
		if strings.TrimSpace(c.AppToken) == "" {
			return fmt.Errorf("APP_TOKEN is required when APP_ENV=production")
		}
		if strings.TrimSpace(c.MiMoAPIKey) == "" {
			return fmt.Errorf("MIMO_API_KEY is required when APP_ENV=production")
		}
		if strings.TrimSpace(c.SessionSigningKey) == "" {
			return fmt.Errorf("SESSION_SIGNING_KEY is required when APP_ENV=production")
		}
		if len(strings.TrimSpace(c.SessionSigningKey)) < 32 {
			return fmt.Errorf("SESSION_SIGNING_KEY must be at least 32 characters")
		}
	}
	return nil
}

func (c *Config) SessionAuthRequired() bool {
	if strings.EqualFold(c.Environment, "production") {
		return true
	}
	mode := strings.ToLower(strings.TrimSpace(c.AuthMode))
	return mode == "session"
}

func (c *Config) LegacyAuthAllowed() bool {
	if strings.EqualFold(c.Environment, "production") {
		return false
	}
	mode := strings.ToLower(strings.TrimSpace(c.AuthMode))
	if mode == "legacy" || mode == "dual" {
		return true
	}
	return mode == ""
}

func (c *Config) OperatorTokenMatches(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	matched := 0
	for _, configured := range []string{c.AppToken, c.SecondaryAppToken} {
		if configured != "" && len(token) == len(configured) {
			matched |= subtle.ConstantTimeCompare([]byte(token), []byte(configured))
		}
	}
	return matched == 1
}

func developmentSessionSigningKey() string {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err == nil {
		return base64.RawURLEncoding.EncodeToString(key)
	}
	return ""
}

func (c *Config) OriginAllowed(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	if !strings.EqualFold(c.Environment, "production") && isLoopbackHost(parsed.Hostname()) {
		return true
	}
	normalized := strings.TrimRight(origin, "/")
	for _, allowed := range c.AllowedOrigins {
		if normalized == strings.TrimRight(allowed, "/") {
			return true
		}
	}
	return false
}

func (c *Config) OriginAllowedForHost(origin, requestHost string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err == nil && parsed.Scheme != "" && strings.EqualFold(parsed.Host, strings.TrimSpace(requestHost)) {
		return true
	}
	return c.OriginAllowed(origin)
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

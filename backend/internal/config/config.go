package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port              string
	Environment       string
	AppToken          string
	AuthMode          string
	SessionSigningKey string
	AllowedOrigins    []string
	DatabaseURL       string
	RedisAddr         string
	MiMoAPIKey        string
	MiMoBaseURL       string
	MiMoModel         string
	MiMoVoice         string
	APISportsAPIKey   string
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
		sessionSigningKey = "qiuqiu-development-session-signing-key-rotate-me"
	}
	return &Config{
		Port:              getEnv("PORT", "8080"),
		Environment:       environment,
		AppToken:          strings.TrimSpace(os.Getenv("APP_TOKEN")),
		AuthMode:          authMode,
		SessionSigningKey: sessionSigningKey,
		AllowedOrigins:    splitCSV(os.Getenv("ALLOWED_ORIGINS")),
		DatabaseURL:       getEnv("DATABASE_URL", ""),
		RedisAddr:         getEnv("REDIS_ADDR", "localhost:6379"),
		MiMoAPIKey:        getEnv("MIMO_API_KEY", ""),
		MiMoBaseURL:       getEnv("MIMO_BASE_URL", "https://api.xiaomimimo.com/v1"),
		MiMoModel:         getEnv("MIMO_MODEL", "mimo-v2.5-pro"),
		MiMoVoice:         getEnv("MIMO_VOICE", "Chloe"),
		APISportsAPIKey:   strings.TrimSpace(os.Getenv("APISPORTS_API_KEY")),
	}
}

func (c *Config) RedisEnabled() bool {
	return c.RedisAddr != ""
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
	mode := strings.ToLower(strings.TrimSpace(c.AuthMode))
	return mode == "session" || (mode == "" && strings.EqualFold(c.Environment, "production"))
}

func (c *Config) LegacyAuthAllowed() bool {
	mode := strings.ToLower(strings.TrimSpace(c.AuthMode))
	if mode == "legacy" || mode == "dual" {
		return true
	}
	return mode == "" && !strings.EqualFold(c.Environment, "production")
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

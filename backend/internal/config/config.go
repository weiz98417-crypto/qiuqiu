package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port           string
	AppToken       string
	RedisAddr      string
	DeepseekAPIKey string
	DeepseekBaseURL string
	ElevenLabsKey  string
}

func Load() *Config {
	return &Config{
		Port:            getEnv("PORT", "8080"),
		AppToken:        getEnv("APP_TOKEN", "qiuqiu-dev-token"),
		RedisAddr:       getEnv("REDIS_ADDR", "localhost:6379"),
		DeepseekAPIKey:  getEnv("DEEPSEEK_API_KEY", ""),
		DeepseekBaseURL: getEnv("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1"),
		ElevenLabsKey:   getEnv("ELEVENLABS_API_KEY", ""),
	}
}

func (c *Config) RedisEnabled() bool {
	return c.RedisAddr != ""
}

func (c *Config) WSReadLimit() int64 {
	return 64 << 10 // 64KB
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

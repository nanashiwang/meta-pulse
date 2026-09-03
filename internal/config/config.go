package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Environment         string
	HTTPAddr            string
	PulseDBDSN          string
	RedisAddr           string
	RedisPassword       string
	RedisDB             int
	NewAPILogDSN        string
	NewAPIInternalURL   string
	ServiceHMACSecret   string
	UserBFFHMACSecret   string
	AdminHMACSecret     string
	IngestBatchSize     int
	SettlementBatchSize int
}

func Load() (Config, error) {
	cfg := Config{
		Environment:         getenv("PULSE_ENV", "development"),
		HTTPAddr:            getenv("PULSE_HTTP_ADDR", ":8088"),
		PulseDBDSN:          os.Getenv("PULSE_DB_DSN"),
		RedisAddr:           getenv("PULSE_REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:       os.Getenv("PULSE_REDIS_PASSWORD"),
		NewAPILogDSN:        os.Getenv("NEWAPI_LOG_DSN"),
		NewAPIInternalURL:   os.Getenv("NEWAPI_INTERNAL_BASE_URL"),
		ServiceHMACSecret:   os.Getenv("PULSE_SERVICE_HMAC_SECRET"),
		UserBFFHMACSecret:   os.Getenv("PULSE_USER_BFF_HMAC_SECRET"),
		AdminHMACSecret:     os.Getenv("PULSE_ADMIN_HMAC_SECRET"),
		IngestBatchSize:     getenvInt("PULSE_INGEST_BATCH_SIZE", 500),
		SettlementBatchSize: getenvInt("PULSE_SETTLEMENT_BATCH_SIZE", 100),
	}

	redisDB, err := strconv.Atoi(getenv("PULSE_REDIS_DB", "0"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid PULSE_REDIS_DB: %w", err)
	}
	cfg.RedisDB = redisDB

	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

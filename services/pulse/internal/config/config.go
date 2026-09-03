package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const minimumProductionSecretLength = 32

type Config struct {
	Environment                 string
	HTTPAddr                    string
	PulseDBDSN                  string
	RedisAddr                   string
	RedisPassword               string
	RedisDB                     int
	NewAPILogDSN                string
	ForumDBDSN                  string
	NewAPIInternalURL           string
	ServiceHMACSecret           string
	UserBFFHMACSecret           string
	AdminHMACSecret             string
	RewardRandomSecret          string
	RewardShadowMode            bool
	IngestBatchSize             int
	SettlementBatchSize         int
	PeriodCloseBatchSize        int
	PeriodCloseRequireWatermark bool
	PeriodRewardsEnabled        bool
	ContentIngestBatchSize      int
	ContentMinPaidContribution  int64
	ContentMaxUserPeriodAmount  int64
	ContentMaxDailyAmount       int64
	TicketThresholdMilli        int64
}

func Load() (Config, error) {
	redisDB, err := getenvInt("PULSE_REDIS_DB", 0, true)
	if err != nil {
		return Config{}, err
	}
	ingestBatchSize, err := getenvInt("PULSE_INGEST_BATCH_SIZE", 500, false)
	if err != nil {
		return Config{}, err
	}
	settlementBatchSize, err := getenvInt("PULSE_SETTLEMENT_BATCH_SIZE", 100, false)
	if err != nil {
		return Config{}, err
	}
	ticketThresholdMilli, err := getenvInt("PULSE_TICKET_THRESHOLD_MILLI", 1000, false)
	if err != nil {
		return Config{}, err
	}
	rewardShadowMode, err := getenvBool("PULSE_REWARD_SHADOW_MODE", true)
	if err != nil {
		return Config{}, err
	}
	periodCloseBatchSize, err := getenvInt("PULSE_PERIOD_CLOSE_BATCH_SIZE", 20, false)
	if err != nil {
		return Config{}, err
	}
	periodCloseRequireWatermark, err := getenvBool("PULSE_PERIOD_CLOSE_REQUIRE_WATERMARK", true)
	if err != nil {
		return Config{}, err
	}
	periodRewardsEnabled, err := getenvBool("PULSE_PERIOD_REWARDS_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	contentIngestBatchSize, err := getenvInt("PULSE_CONTENT_INGEST_BATCH_SIZE", 100, false)
	if err != nil {
		return Config{}, err
	}
	contentMinPaidContribution, err := getenvInt64("PULSE_CONTENT_MIN_PAID_CONTRIBUTION_MILLI", 1000, true)
	if err != nil {
		return Config{}, err
	}
	contentMaxUserPeriodAmount, err := getenvInt64("PULSE_CONTENT_MAX_USER_PERIOD_AMOUNT", 100, false)
	if err != nil {
		return Config{}, err
	}
	contentMaxDailyAmount, err := getenvInt64("PULSE_CONTENT_MAX_DAILY_AMOUNT", 1000, false)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Environment:                 strings.ToLower(getenv("PULSE_ENV", "development")),
		HTTPAddr:                    getenv("PULSE_HTTP_ADDR", ":8088"),
		PulseDBDSN:                  os.Getenv("PULSE_DB_DSN"),
		RedisAddr:                   getenv("PULSE_REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:               os.Getenv("PULSE_REDIS_PASSWORD"),
		RedisDB:                     redisDB,
		NewAPILogDSN:                os.Getenv("NEWAPI_LOG_DSN"),
		ForumDBDSN:                  os.Getenv("FORUM_DB_DSN"),
		NewAPIInternalURL:           os.Getenv("NEWAPI_INTERNAL_BASE_URL"),
		ServiceHMACSecret:           os.Getenv("PULSE_SERVICE_HMAC_SECRET"),
		UserBFFHMACSecret:           os.Getenv("PULSE_USER_BFF_HMAC_SECRET"),
		AdminHMACSecret:             os.Getenv("PULSE_ADMIN_HMAC_SECRET"),
		RewardRandomSecret:          getenv("PULSE_REWARD_RANDOM_SECRET", "replace-me"),
		RewardShadowMode:            rewardShadowMode,
		IngestBatchSize:             ingestBatchSize,
		SettlementBatchSize:         settlementBatchSize,
		PeriodCloseBatchSize:        periodCloseBatchSize,
		PeriodCloseRequireWatermark: periodCloseRequireWatermark,
		PeriodRewardsEnabled:        periodRewardsEnabled,
		ContentIngestBatchSize:      contentIngestBatchSize,
		ContentMinPaidContribution:  contentMinPaidContribution,
		ContentMaxUserPeriodAmount:  contentMaxUserPeriodAmount,
		ContentMaxDailyAmount:       contentMaxDailyAmount,
		TicketThresholdMilli:        int64(ticketThresholdMilli),
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate contains process-independent validation. Worker-only dependencies
// are validated by ValidateWorker so the API does not gain unnecessary access
// to new-api LOG_DB.
func (cfg Config) Validate() error {
	var errs []error
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		errs = append(errs, errors.New("PULSE_HTTP_ADDR is required"))
	}
	if strings.TrimSpace(cfg.PulseDBDSN) == "" {
		errs = append(errs, errors.New("PULSE_DB_DSN is required"))
	}
	if strings.TrimSpace(cfg.RedisAddr) == "" {
		errs = append(errs, errors.New("PULSE_REDIS_ADDR is required"))
	}
	if cfg.RedisDB < 0 {
		errs = append(errs, errors.New("PULSE_REDIS_DB must be non-negative"))
	}
	if cfg.IngestBatchSize <= 0 {
		errs = append(errs, errors.New("PULSE_INGEST_BATCH_SIZE must be positive"))
	}
	if cfg.SettlementBatchSize <= 0 {
		errs = append(errs, errors.New("PULSE_SETTLEMENT_BATCH_SIZE must be positive"))
	}
	if cfg.PeriodCloseBatchSize <= 0 {
		errs = append(errs, errors.New("PULSE_PERIOD_CLOSE_BATCH_SIZE must be positive"))
	}
	if cfg.ContentIngestBatchSize <= 0 || cfg.ContentMinPaidContribution < 0 || cfg.ContentMaxUserPeriodAmount <= 0 || cfg.ContentMaxDailyAmount <= 0 {
		errs = append(errs, errors.New("content reward limits must be valid"))
	}
	if cfg.TicketThresholdMilli <= 0 {
		errs = append(errs, errors.New("PULSE_TICKET_THRESHOLD_MILLI must be positive"))
	}

	if cfg.Environment == "production" {
		for name, secret := range map[string]string{
			"PULSE_SERVICE_HMAC_SECRET":  cfg.ServiceHMACSecret,
			"PULSE_USER_BFF_HMAC_SECRET": cfg.UserBFFHMACSecret,
			"PULSE_ADMIN_HMAC_SECRET":    cfg.AdminHMACSecret,
			"PULSE_REWARD_RANDOM_SECRET": cfg.RewardRandomSecret,
		} {
			if len(secret) < minimumProductionSecretLength || secret == "replace-me" {
				errs = append(errs, fmt.Errorf("%s must be at least %d bytes and cannot use a placeholder in production", name, minimumProductionSecretLength))
			}
		}
	}
	return errors.Join(errs...)
}

func (cfg Config) ValidateWorker() error {
	var errs []error
	if strings.TrimSpace(cfg.NewAPILogDSN) == "" {
		errs = append(errs, errors.New("NEWAPI_LOG_DSN is required for worker"))
	}
	if strings.TrimSpace(cfg.NewAPIInternalURL) == "" {
		errs = append(errs, errors.New("NEWAPI_INTERNAL_BASE_URL is required for worker"))
	}
	if strings.TrimSpace(cfg.ServiceHMACSecret) == "" {
		errs = append(errs, errors.New("PULSE_SERVICE_HMAC_SECRET is required for worker"))
	}
	return errors.Join(errs...)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int, allowZero bool) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 || (!allowZero && parsed == 0) {
		return 0, fmt.Errorf("invalid %s: must be %s integer", key, map[bool]string{true: "a non-negative", false: "a positive"}[allowZero])
	}
	return parsed, nil
}

func getenvInt64(key string, fallback int64, allowZero bool) (int64, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 || (!allowZero && parsed == 0) {
		return 0, fmt.Errorf("invalid %s: must be %s integer", key, map[bool]string{true: "a non-negative", false: "a positive"}[allowZero])
	}
	return parsed, nil
}

func getenvBool(key string, fallback bool) (bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s: must be true or false", key)
	}
	return parsed, nil
}

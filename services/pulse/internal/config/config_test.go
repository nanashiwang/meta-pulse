package config

import (
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{
		Environment:                "development",
		HTTPAddr:                   ":8088",
		PulseDBDSN:                 "pulse:secret@tcp(mysql:3306)/meta_pulse",
		RedisAddr:                  "redis:6379",
		IngestBatchSize:            250,
		SettlementBatchSize:        100,
		PeriodCloseBatchSize:       20,
		ContentIngestBatchSize:     100,
		ContentMaxUserPeriodAmount: 100,
		ContentMaxDailyAmount:      1000,
		TicketThresholdMilli:       1000,
		RewardRandomSecret:         strings.Repeat("r", 32),
	}
}

func TestValidateRequiresRuntimeDependencies(t *testing.T) {
	cfg := validConfig()
	cfg.PulseDBDSN = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "PULSE_DB_DSN") {
		t.Fatalf("Validate() error = %v, want missing PULSE_DB_DSN", err)
	}
}

func TestValidateProductionSecretsFailClosed(t *testing.T) {
	cfg := validConfig()
	cfg.Environment = "production"
	cfg.ServiceHMACSecret = "replace-me"
	cfg.ForumHMACSecret = "short"
	cfg.UserBFFHMACSecret = "short"
	cfg.AdminHMACSecret = ""
	if err := cfg.ValidateAPI(); err == nil {
		t.Fatal("production placeholders were accepted")
	}

	cfg.ServiceHMACSecret = strings.Repeat("s", 32)
	cfg.ForumHMACSecret = strings.Repeat("f", 32)
	cfg.UserBFFHMACSecret = strings.Repeat("u", 32)
	cfg.AdminHMACSecret = strings.Repeat("a", 32)
	if err := cfg.ValidateAPI(); err != nil {
		t.Fatalf("valid production config rejected: %v", err)
	}
}

func TestValidateLogReaderOnlyRequiresLogDSN(t *testing.T) {
	cfg := validConfig()
	cfg.NewAPILogDSN = "readonly@tcp(newapi-mysql:3306)/new_api"
	if err := cfg.ValidateLogReader(); err != nil {
		t.Fatalf("log reader should not require Benefit settings: %v", err)
	}
	cfg.NewAPILogDSN = ""
	if err := cfg.ValidateLogReader(); err == nil || !strings.Contains(err.Error(), "NEWAPI_LOG_DSN") {
		t.Fatalf("missing log DSN error = %v", err)
	}
}

func TestValidateWorkerUsesReadOnlyIntegrationSettings(t *testing.T) {
	cfg := validConfig()
	if err := cfg.ValidateWorker(); err == nil {
		t.Fatal("worker config without new-api integration was accepted")
	}
	cfg.NewAPILogDSN = "readonly@tcp(newapi-mysql:3306)/new_api"
	cfg.NewAPIInternalURL = "http://new-api:3000"
	cfg.ServiceHMACSecret = "secret"
	if err := cfg.ValidateWorker(); err != nil {
		t.Fatalf("valid worker config rejected: %v", err)
	}
}

func TestRetiredUsageEnvironmentCannotBlockOrReenableWorker(t *testing.T) {
	t.Setenv("PULSE_DB_DSN", "pulse:test@tcp(localhost:3306)/pulse_test")
	for _, value := range []string{"", "invalid", "5001", "true"} {
		t.Setenv("PULSE_INGEST_BATCH_SIZE", value)
		t.Setenv("PULSE_PERIOD_CLOSE_REQUIRE_WATERMARK", value)
		t.Setenv("PULSE_PERIOD_REWARDS_ENABLED", value)
		t.Setenv("PULSE_CONTENT_MIN_PAID_CONTRIBUTION_MILLI", value)
		t.Setenv("NEWAPI_LOG_DSN", "invalid-retired-dsn")
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.NewAPILogDSN != "" || cfg.PeriodCloseRequireWatermark || cfg.PeriodRewardsEnabled || cfg.ContentMinPaidContribution != 0 {
			t.Fatal("retired configuration reenabled")
		}
	}
	cfg := validConfig()
	cfg.NewAPILogDSN = ""
	cfg.ServiceHMACSecret = strings.Repeat("s", 32)
	cfg.NewAPIInternalURL = "http://new-api:3000"
	if err := cfg.ValidateWorker(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProductionRejectsWhitespacePaddedPreviousSecret(t *testing.T) {
	cfg := validConfig()
	cfg.Environment = "production"
	cfg.ServiceHMACSecretPrevious = strings.Repeat(" ", 40) + "replace-me" + strings.Repeat(" ", 40)
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "PULSE_SERVICE_HMAC_SECRET_PREVIOUS") {
		t.Fatalf("whitespace-padded previous secret error = %v", err)
	}
}

func TestValidateRejectsDuplicatePreviousSecret(t *testing.T) {
	cfg := validConfig()
	cfg.ServiceHMACSecret = "same-secret"
	cfg.ServiceHMACSecretPrevious = " same-secret "
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "PULSE_SERVICE_HMAC_SECRET") {
		t.Fatalf("duplicate rotation secret error = %v", err)
	}
}

func TestSecretPairsKeepActiveBeforePrevious(t *testing.T) {
	cfg := validConfig()
	cfg.ServiceHMACSecret = "active"
	cfg.ServiceHMACSecretPrevious = "previous"
	secrets := cfg.ServiceHMACSecrets()
	if len(secrets) != 2 || string(secrets[0]) != "active" || string(secrets[1]) != "previous" {
		t.Fatalf("unexpected secret order: %#v", secrets)
	}
}

func TestValidateRejectsSecretReuseAcrossTrustRoles(t *testing.T) {
	cfg := validConfig()
	cfg.ServiceHMACSecret = "shared-secret"
	cfg.ForumHMACSecret = "shared-secret"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "must not reuse") {
		t.Fatalf("cross-role secret reuse error = %v", err)
	}
}

func TestForumSecretPairsKeepActiveBeforePrevious(t *testing.T) {
	cfg := validConfig()
	cfg.ForumHMACSecret = "forum-active"
	cfg.ForumHMACSecretPrevious = "forum-previous"
	secrets := cfg.ForumHMACSecrets()
	if len(secrets) != 2 || string(secrets[0]) != "forum-active" || string(secrets[1]) != "forum-previous" {
		t.Fatalf("unexpected forum secret order: %#v", secrets)
	}
}

func TestProductionProcessRolesUseLeastPrivilege(t *testing.T) {
	cfg := validConfig()
	cfg.Environment = "production"
	cfg.NewAPILogDSN = "readonly@tcp(newapi-mysql:3306)/new_api"
	cfg.NewAPIInternalURL = "http://new-api:3000"
	cfg.ServiceHMACSecret = strings.Repeat("s", 32)
	if err := cfg.ValidateWorker(); err != nil {
		t.Fatalf("worker should not require BFF/admin keys: %v", err)
	}
	if err := cfg.ValidateAPI(); err == nil {
		t.Fatal("API accepted missing signing keys")
	}
	cfg.ServiceHMACSecret = ""
	cfg.RewardRandomSecret = ""
	if err := cfg.ValidateRole("tool"); err != nil {
		t.Fatalf("migration tool required signing keys: %v", err)
	}
	if err := cfg.ValidateWorker(); err == nil {
		t.Fatal("worker accepted missing signing/random keys")
	}
	if err := cfg.ValidateRole("unknown"); err == nil {
		t.Fatal("unknown role accepted")
	}
}

func TestLoadProductionWorkerWithoutAPISecrets(t *testing.T) {
	t.Setenv("PULSE_ENV", "production")
	t.Setenv("PULSE_DB_DSN", "pulse@tcp(mysql:3306)/meta_pulse")
	t.Setenv("NEWAPI_LOG_DSN", "readonly@tcp(logs:3306)/logs")
	t.Setenv("NEWAPI_INTERNAL_BASE_URL", "http://new-api:3000")
	t.Setenv("PULSE_SERVICE_HMAC_SECRET", strings.Repeat("s", 32))
	t.Setenv("PULSE_REWARD_RANDOM_SECRET", strings.Repeat("r", 32))
	for _, key := range []string{"PULSE_FORUM_HMAC_SECRET", "PULSE_USER_BFF_HMAC_SECRET", "PULSE_ADMIN_HMAC_SECRET", "PULSE_SERVICE_HMAC_SECRET_PREVIOUS", "PULSE_FORUM_HMAC_SECRET_PREVIOUS", "PULSE_USER_BFF_HMAC_SECRET_PREVIOUS", "PULSE_ADMIN_HMAC_SECRET_PREVIOUS"} {
		t.Setenv(key, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateWorker(); err != nil {
		t.Fatal(err)
	}
}

func TestAPIStartsWithoutGrantAuthority(t *testing.T) {
	cfg := validConfig()
	cfg.Environment = "production"
	cfg.ForumHMACSecret = strings.Repeat("f", 32)
	cfg.UserBFFHMACSecret = strings.Repeat("u", 32)
	cfg.AdminHMACSecret = strings.Repeat("a", 32)
	if err := cfg.ValidateAPI(); err != nil {
		t.Fatal(err)
	}
	if cfg.ActionsEnabled {
		t.Fatal("actions must default closed")
	}
	cfg.ActionsEnabled = true
	if err := cfg.ValidateAPI(); err == nil {
		t.Fatal("enabled without quota conversion")
	}
	cfg.QuotaPerUnit = 500000
	if err := cfg.ValidateAPI(); err != nil {
		t.Fatal(err)
	}
	cfg.QuotaPerUnit = 1 << 53
	if err := cfg.ValidateAPI(); err == nil {
		t.Fatal("unsafe JSON integer conversion accepted")
	}
}

func TestNewRewardAuthoritiesCannotReuseKeys(t *testing.T) {
	for _, mutate := range []func(*Config){
		func(c *Config) { c.CommunityBFFHMACSecret = c.ServiceHMACSecret },
		func(c *Config) { c.RollbackHMACSecret = c.ServiceHMACSecret },
		func(c *Config) { c.CommunityBFFHMACSecretPrevious = c.ServiceHMACSecret },
		func(c *Config) { c.RollbackHMACSecretPrevious = c.ServiceHMACSecret },
	} {
		cfg := validConfig()
		cfg.ServiceHMACSecret = strings.Repeat("s", 32)
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatal("cross-role key reuse accepted")
		}
	}
}

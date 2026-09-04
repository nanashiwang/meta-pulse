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
		IngestBatchSize:            500,
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
	cfg.UserBFFHMACSecret = "short"
	cfg.AdminHMACSecret = ""
	if err := cfg.ValidateAPI(); err == nil {
		t.Fatal("production placeholders were accepted")
	}

	cfg.ServiceHMACSecret = strings.Repeat("s", 32)
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

func TestLoadRejectsInvalidIntegerInsteadOfFallingBack(t *testing.T) {
	t.Setenv("PULSE_ENV", "development")
	t.Setenv("PULSE_DB_DSN", "pulse@tcp(mysql:3306)/meta_pulse")
	t.Setenv("PULSE_INGEST_BATCH_SIZE", "invalid")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PULSE_INGEST_BATCH_SIZE") {
		t.Fatalf("Load() error = %v, want invalid integer", err)
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
	for _, key := range []string{"PULSE_SERVICE_HMAC_SECRET", "PULSE_REWARD_RANDOM_SECRET"} {
		t.Setenv(key, strings.Repeat("s", 32))
	}
	for _, key := range []string{"PULSE_USER_BFF_HMAC_SECRET", "PULSE_ADMIN_HMAC_SECRET", "PULSE_SERVICE_HMAC_SECRET_PREVIOUS", "PULSE_USER_BFF_HMAC_SECRET_PREVIOUS", "PULSE_ADMIN_HMAC_SECRET_PREVIOUS"} {
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

package config

import (
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{
		Environment:          "development",
		HTTPAddr:             ":8088",
		PulseDBDSN:           "pulse:secret@tcp(mysql:3306)/meta_pulse",
		RedisAddr:            "redis:6379",
		IngestBatchSize:      500,
		SettlementBatchSize:  100,
		TicketThresholdMilli: 1000,
		RewardRandomSecret:   strings.Repeat("r", 32),
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
	if err := cfg.Validate(); err == nil {
		t.Fatal("production placeholders were accepted")
	}

	cfg.ServiceHMACSecret = strings.Repeat("s", 32)
	cfg.UserBFFHMACSecret = strings.Repeat("u", 32)
	cfg.AdminHMACSecret = strings.Repeat("a", 32)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid production config rejected: %v", err)
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

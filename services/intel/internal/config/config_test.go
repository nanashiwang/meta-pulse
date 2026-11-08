package config

import (
	"strings"
	"testing"
)

func TestAdminSecrets(t *testing.T) {
	t.Setenv("INTEL_DB_DSN", "u:p@tcp(h:3306)/d")
	c, err := Load()
	if err != nil || len(c.AdminSecrets) != 0 {
		t.Fatalf("no secret: %v %d", err, len(c.AdminSecrets))
	}
	t.Setenv("INTEL_ADMIN_HMAC_SECRET", "short")
	if _, err := Load(); err == nil {
		t.Fatal("short secret accepted")
	}
	t.Setenv("INTEL_ADMIN_HMAC_SECRET", strings.Repeat("a", 32))
	t.Setenv("INTEL_ADMIN_HMAC_SECRET_PREVIOUS", strings.Repeat("b", 32))
	c, err = Load()
	if err != nil || len(c.AdminSecrets) != 2 || string(c.AdminSecrets[0]) != strings.Repeat("a", 32) {
		t.Fatalf("rotation: %v %d", err, len(c.AdminSecrets))
	}
	t.Setenv("INTEL_ADMIN_HMAC_SECRET", "")
	if _, err := Load(); err == nil {
		t.Fatal("previous without active accepted")
	}
}

func TestDurations(t *testing.T) {
	t.Setenv("INTEL_DB_DSN", "x")
	t.Setenv("INTEL_SCORE_INTERVAL", "10s")
	if _, err := Load(); err == nil {
		t.Fatal("interval below minimum accepted")
	}
}

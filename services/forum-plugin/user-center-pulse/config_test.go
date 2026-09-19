package pulse_user_center

import (
	"strings"
	"testing"
)

func validPluginConfig() *Config {
	return &Config{
		NewAPIBaseURL:   "https://api.example.test",
		PulseBaseURL:    "https://pulse.example.test",
		SSOHMACSecret:   strings.Repeat("s", minimumConfigSecretLength),
		PulseHMACSecret: strings.Repeat("p", minimumConfigSecretLength),
		NonceRedisURL:   "redis://127.0.0.1:6379/0",
	}
}

func TestValidateConfigRejectsUnsafeURLsAndSecrets(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"missing new-api URL", func(c *Config) { c.NewAPIBaseURL = "" }},
		{"URL credentials", func(c *Config) { c.NewAPIBaseURL = "https://user:pass@example.test" }},
		{"URL query", func(c *Config) { c.PulseBaseURL = "https://pulse.example.test/?next=1" }},
		{"short secret", func(c *Config) { c.SSOHMACSecret = "short" }},
		{"duplicate previous secret", func(c *Config) { c.SSOHMACSecretPrevious = c.SSOHMACSecret }},
		{"SSO and Profile secrets reused", func(c *Config) { c.PulseHMACSecret = c.SSOHMACSecret }},
		{"short admin key", func(c *Config) { c.AdminHMACSecret = "short" }},
		{"admin and SSO keys reused", func(c *Config) { c.AdminHMACSecret = c.SSOHMACSecret }},
		{"admin and profile keys reused", func(c *Config) { c.AdminHMACSecret = c.PulseHMACSecret }},
		{"admin and community keys reused", func(c *Config) {
			c.CommunityBFFHMACSecret = strings.Repeat("c", 32)
			c.AdminHMACSecret = c.CommunityBFFHMACSecret
		}},
		{"short community key", func(c *Config) { c.CommunityBFFHMACSecret = "short" }},
		{"community and SSO keys reused", func(c *Config) { c.CommunityBFFHMACSecret = c.SSOHMACSecret }},
		{"community and profile keys reused", func(c *Config) { c.CommunityBFFHMACSecret = c.PulseHMACSecret }},
		{"community and old SSO keys reused", func(c *Config) {
			c.SSOHMACSecretPrevious = strings.Repeat("o", 32)
			c.CommunityBFFHMACSecret = c.SSOHMACSecretPrevious
		}},
		{"old SSO and Profile secrets reused", func(c *Config) {
			c.SSOHMACSecretPrevious = c.PulseHMACSecret
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := validPluginConfig()
			tc.mutate(config)
			if err := validateConfig(config); err == nil {
				t.Fatal("unsafe plugin config was accepted")
			}
		})
	}
}

func TestValidateConfigAcceptsRotationPair(t *testing.T) {
	config := validPluginConfig()
	config.SSOHMACSecretPrevious = strings.Repeat("o", minimumConfigSecretLength)
	if err := validateConfig(config); err != nil {
		t.Fatalf("valid rotation config rejected: %v", err)
	}
}

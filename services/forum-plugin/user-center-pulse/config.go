package pulse_user_center

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/apache/answer/plugin"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/i18n"
)

// Config is edited by operators in the Answer admin panel.
//
// The HMAC secret is never committed; it is entered at runtime and stored by
// Answer's own plugin config storage.
type Config struct {
	NewAPIBaseURL         string `json:"newapi_base_url"`
	PulseBaseURL          string `json:"pulse_base_url"`
	SSOHMACSecret         string `json:"sso_hmac_secret"`
	SSOHMACSecretPrevious string `json:"sso_hmac_secret_previous"`
	PulseHMACSecret       string `json:"pulse_hmac_secret"`
	NonceRedisURL         string `json:"nonce_redis_url"`
	LevelBadgeEnabled     bool   `json:"level_badge_enabled"`
}

func (uc *UserCenter) ConfigFields() []plugin.ConfigField {
	config := uc.configSnapshot()
	return []plugin.ConfigField{
		{
			Name:        "newapi_base_url",
			Type:        plugin.ConfigTypeInput,
			Title:       plugin.MakeTranslator(i18n.ConfigNewAPIBaseURLTitle),
			Description: plugin.MakeTranslator(i18n.ConfigNewAPIBaseURLDescription),
			Required:    true,
			UIOptions: plugin.ConfigFieldUIOptions{
				InputType: plugin.InputTypeText,
			},
			Value: config.NewAPIBaseURL,
		},
		{
			Name:        "pulse_base_url",
			Type:        plugin.ConfigTypeInput,
			Title:       plugin.MakeTranslator(i18n.ConfigPulseBaseURLTitle),
			Description: plugin.MakeTranslator(i18n.ConfigPulseBaseURLDescription),
			Required:    true,
			UIOptions: plugin.ConfigFieldUIOptions{
				InputType: plugin.InputTypeText,
			},
			Value: config.PulseBaseURL,
		},
		{
			Name:        "sso_hmac_secret",
			Type:        plugin.ConfigTypeInput,
			Title:       plugin.MakeTranslator(i18n.ConfigSSOHMACSecretTitle),
			Description: plugin.MakeTranslator(i18n.ConfigSSOHMACSecretDescription),
			Required:    true,
			UIOptions: plugin.ConfigFieldUIOptions{
				InputType: plugin.InputTypePassword,
			},
			Value: config.SSOHMACSecret,
		},
		{
			Name:        "sso_hmac_secret_previous",
			Type:        plugin.ConfigTypeInput,
			Title:       plugin.MakeTranslator(i18n.ConfigSSOHMACSecretPreviousTitle),
			Description: plugin.MakeTranslator(i18n.ConfigSSOHMACSecretPreviousDescription),
			UIOptions: plugin.ConfigFieldUIOptions{
				InputType: plugin.InputTypePassword,
			},
			Value: config.SSOHMACSecretPrevious,
		},
		{
			Name:        "pulse_hmac_secret",
			Type:        plugin.ConfigTypeInput,
			Title:       plugin.MakeTranslator(i18n.ConfigPulseHMACSecretTitle),
			Description: plugin.MakeTranslator(i18n.ConfigPulseHMACSecretDescription),
			Required:    true,
			UIOptions: plugin.ConfigFieldUIOptions{
				InputType: plugin.InputTypePassword,
			},
			Value: config.PulseHMACSecret,
		},
		{
			Name:        "nonce_redis_url",
			Type:        plugin.ConfigTypeInput,
			Title:       plugin.MakeTranslator(i18n.ConfigNonceRedisURLTitle),
			Description: plugin.MakeTranslator(i18n.ConfigNonceRedisURLDescription),
			Required:    true,
			UIOptions: plugin.ConfigFieldUIOptions{
				InputType: plugin.InputTypePassword,
			},
			Value: config.NonceRedisURL,
		},
		{
			Name:        "level_badge_enabled",
			Type:        plugin.ConfigTypeSwitch,
			Title:       plugin.MakeTranslator(i18n.ConfigLevelBadgeEnabledTitle),
			Description: plugin.MakeTranslator(i18n.ConfigLevelBadgeEnabledDescription),
			UIOptions: plugin.ConfigFieldUIOptions{
				Label: plugin.MakeTranslator(i18n.ConfigLevelBadgeEnabledLabel),
			},
			Value: config.LevelBadgeEnabled,
		},
	}
}

const minimumConfigSecretLength = 32

func validateConfig(c *Config) error {
	if c == nil {
		return errors.New("forum plugin config is nil")
	}
	for name, raw := range map[string]string{
		"newapi_base_url": c.NewAPIBaseURL,
		"pulse_base_url":  c.PulseBaseURL,
	} {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" || (parsed.Path != "" && parsed.Path != "/") {
			return fmt.Errorf("%s must be an absolute root http(s) URL without credentials, query, or fragment", name)
		}
		if name == "newapi_base_url" && parsed.Scheme != "https" {
			return errors.New("newapi_base_url must use https")
		}
	}
	for name, secret := range map[string]string{
		"sso_hmac_secret":   c.SSOHMACSecret,
		"pulse_hmac_secret": c.PulseHMACSecret,
	} {
		if !usableConfigSecret(secret) {
			return fmt.Errorf("%s must be at least %d bytes and cannot be a placeholder", name, minimumConfigSecretLength)
		}
	}
	if previous := strings.TrimSpace(c.SSOHMACSecretPrevious); previous != "" {
		if !usableConfigSecret(previous) || previous == strings.TrimSpace(c.SSOHMACSecret) {
			return errors.New("sso_hmac_secret_previous is invalid or duplicates the active secret")
		}
	}
	secretOwners := make(map[string]string)
	for _, item := range []struct{ name, value string }{
		{"sso_hmac_secret", c.SSOHMACSecret},
		{"sso_hmac_secret_previous", c.SSOHMACSecretPrevious},
		{"pulse_hmac_secret", c.PulseHMACSecret},
	} {
		value := strings.TrimSpace(item.value)
		if value == "" {
			continue
		}
		if owner, exists := secretOwners[value]; exists {
			return fmt.Errorf("%s must not reuse %s", item.name, owner)
		}
		secretOwners[value] = item.name
	}
	return nil
}

func usableConfigSecret(secret string) bool {
	secret = strings.TrimSpace(secret)
	return len(secret) >= minimumConfigSecretLength && secret != "replace-me"
}

func (uc *UserCenter) ConfigReceiver(config []byte) error {
	c := &Config{}
	decoder := json.NewDecoder(bytes.NewReader(config))
	if err := decoder.Decode(c); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != nil && !errors.Is(err, io.EOF) {
		return err
	} else if err == nil {
		return errors.New("trailing JSON value")
	}
	if err := validateConfig(c); err != nil {
		return err
	}
	c.NewAPIBaseURL = strings.TrimRight(strings.TrimSpace(c.NewAPIBaseURL), "/")
	c.PulseBaseURL = strings.TrimRight(strings.TrimSpace(c.PulseBaseURL), "/")
	c.SSOHMACSecret = strings.TrimSpace(c.SSOHMACSecret)
	c.SSOHMACSecretPrevious = strings.TrimSpace(c.SSOHMACSecretPrevious)
	c.PulseHMACSecret = strings.TrimSpace(c.PulseHMACSecret)
	c.NonceRedisURL = strings.TrimSpace(c.NonceRedisURL)

	logins, err := NewRedisNonceStore(c.NonceRedisURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := logins.Ping(ctx); err != nil {
		_ = logins.Close()
		return fmt.Errorf("connect forum login redis: %w", err)
	}

	factory := uc.newBindingGuard
	if factory == nil {
		factory = NewMySQLBindingGuard
	}
	guard, err := factory(strings.TrimSpace(os.Getenv("FORUM_BINDING_GUARD_DSN")))
	if err != nil {
		_ = logins.Close()
		return err
	}
	if guard == nil {
		_ = logins.Close()
		return errors.New("forum binding guard factory returned nil")
	}
	if err := guard.Ensure(ctx); err != nil {
		_ = guard.Close()
		_ = logins.Close()
		return fmt.Errorf("install forum binding guard: %w", err)
	}
	if err := guard.Ready(ctx); err != nil {
		_ = guard.Close()
		_ = logins.Close()
		return fmt.Errorf("verify forum binding guard: %w", err)
	}

	uc.runtimeMu.Lock()
	oldLogins, oldGuard := uc.Logins, uc.Guard
	uc.Config = c
	uc.Client = NewPulseClient(c)
	uc.Logins = logins
	uc.Guard = guard
	uc.runtimeMu.Unlock()
	if closer, ok := oldLogins.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	if oldGuard != nil {
		_ = oldGuard.Close()
	}
	return nil
}

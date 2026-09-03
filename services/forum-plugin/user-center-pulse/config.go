package pulse_user_center

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/apache/answer/plugin"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/i18n"
)

// Config is edited by operators in the Answer admin panel.
//
// The HMAC secret is never committed; it is entered at runtime and stored by
// Answer's own plugin config storage.
type Config struct {
	NewAPIBaseURL     string `json:"newapi_base_url"`
	PulseBaseURL      string `json:"pulse_base_url"`
	SSOHMACSecret     string `json:"sso_hmac_secret"`
	PulseHMACSecret   string `json:"pulse_hmac_secret"`
	NonceRedisURL     string `json:"nonce_redis_url"`
	LevelBadgeEnabled bool   `json:"level_badge_enabled"`
}

func (uc *UserCenter) ConfigFields() []plugin.ConfigField {
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
			Value: uc.Config.NewAPIBaseURL,
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
			Value: uc.Config.PulseBaseURL,
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
			Value: uc.Config.SSOHMACSecret,
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
			Value: uc.Config.PulseHMACSecret,
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
			Value: uc.Config.NonceRedisURL,
		},
		{
			Name:        "level_badge_enabled",
			Type:        plugin.ConfigTypeSwitch,
			Title:       plugin.MakeTranslator(i18n.ConfigLevelBadgeEnabledTitle),
			Description: plugin.MakeTranslator(i18n.ConfigLevelBadgeEnabledDescription),
			UIOptions: plugin.ConfigFieldUIOptions{
				Label: plugin.MakeTranslator(i18n.ConfigLevelBadgeEnabledLabel),
			},
			Value: uc.Config.LevelBadgeEnabled,
		},
	}
}

func (uc *UserCenter) ConfigReceiver(config []byte) error {
	c := &Config{}
	if err := json.Unmarshal(config, c); err != nil {
		return err
	}
	nonces, err := NewRedisNonceStore(c.NonceRedisURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := nonces.Ping(ctx); err != nil {
		_ = nonces.Close()
		return fmt.Errorf("connect forum nonce redis: %w", err)
	}
	oldNonces := uc.Nonces
	uc.Config = c
	uc.Client = NewPulseClient(c)
	uc.Nonces = nonces
	if closer, ok := oldNonces.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	return nil
}

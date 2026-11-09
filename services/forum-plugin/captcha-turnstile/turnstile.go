// Package turnstile implements Answer's native captcha contract.
package turnstile

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/apache/answer/plugin"
)

const slug = "metar_turnstile"
const action = "answer_captcha"
const verifyEndpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type config struct {
	SiteKey   string `json:"site_key"`
	SecretKey string `json:"secret_key"`
	Hostnames string `json:"hostnames"`
}

type Captcha struct {
	mu     sync.RWMutex
	config config
	client *http.Client
}

func init() { plugin.Register(newCaptcha()) }
func newCaptcha() *Captcha {
	return &Captcha{client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func label(text string) plugin.Translator {
	return plugin.Translator{Fn: func(*plugin.GinContext) string { return text }}
}
func (c *Captcha) Info() plugin.Info {
	return plugin.Info{SlugName: slug, Name: label("Cloudflare Turnstile"), Description: label("社区人机验证 / Community CAPTCHA"), Author: "nanashiwang", Version: "0.1.0", Link: "https://github.com/nanashiwang/meta-pulse"}
}
func (c *Captcha) snapshot() config { c.mu.RLock(); defer c.mu.RUnlock(); return c.config }
func (c *Captcha) ConfigFields() []plugin.ConfigField {
	cfg := c.snapshot()
	return []plugin.ConfigField{
		{Name: "site_key", Type: plugin.ConfigTypeInput, Title: label("Site Key（公开站点密钥）"), Required: true, Value: cfg.SiteKey, UIOptions: plugin.ConfigFieldUIOptions{InputType: plugin.InputTypeText}},
		{Name: "secret_key", Type: plugin.ConfigTypeInput, Title: label("Secret Key（服务端密钥）"), Required: true, Value: cfg.SecretKey, UIOptions: plugin.ConfigFieldUIOptions{InputType: plugin.InputTypePassword}},
		{Name: "hostnames", Type: plugin.ConfigTypeInput, Title: label("允许的域名 / Allowed hostnames"), Description: label("精确域名，逗号分隔；例如 metarhub.com,www.metarhub.com；不要填写协议、路径或通配符"), Required: true, Value: cfg.Hostnames, UIOptions: plugin.ConfigFieldUIOptions{InputType: plugin.InputTypeText}},
	}
}
func validHost(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, part := range strings.Split(host, ".") {
		if len(part) == 0 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}
func (c *Captcha) ConfigReceiver(raw []byte) error {
	var cfg config
	if json.Unmarshal(raw, &cfg) != nil {
		return errors.New("invalid Turnstile configuration")
	}
	cfg.SiteKey = strings.TrimSpace(cfg.SiteKey)
	cfg.SecretKey = strings.TrimSpace(cfg.SecretKey)
	cfg.Hostnames = strings.ToLower(strings.TrimSpace(cfg.Hostnames))
	// Answer may load an empty configuration before the operator configures it.
	if cfg != (config{}) {
		if cfg.SiteKey == "" || cfg.SecretKey == "" || cfg.Hostnames == "" {
			return errors.New("Turnstile requires Site Key, Secret Key and allowed hostnames")
		}
		hosts := strings.Split(cfg.Hostnames, ",")
		for i, h := range hosts {
			hosts[i] = strings.TrimSpace(h)
			if !validHost(hosts[i]) {
				return errors.New("Turnstile hostnames must be exact DNS names")
			}
		}
		cfg.Hostnames = strings.Join(hosts, ",")
	}
	c.mu.Lock()
	c.config = cfg
	c.mu.Unlock()
	return nil
}
func (c *Captcha) GetConfig() string {
	cfg := c.snapshot()
	data, _ := json.Marshal(map[string]string{"key": cfg.SiteKey, "action": action})
	return string(data)
}
func (c *Captcha) Create() (string, string) { return "", "" }
func (c *Captcha) Verify(_ string, token string) bool {
	cfg := c.snapshot()
	if cfg.SiteKey == "" || cfg.SecretKey == "" || cfg.Hostnames == "" || strings.TrimSpace(token) == "" || len(token) > 2048 {
		return false
	}
	resp, err := c.client.PostForm(verifyEndpoint, url.Values{"secret": {cfg.SecretKey}, "response": {token}})
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16385))
	if err != nil || len(raw) > 16384 {
		return false
	}
	var result struct {
		Success  bool   `json:"success"`
		Hostname string `json:"hostname"`
		Action   string `json:"action"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Success || result.Action != action {
		return false
	}
	for _, host := range strings.Split(cfg.Hostnames, ",") {
		if result.Hostname == host {
			return true
		}
	}
	return false
}

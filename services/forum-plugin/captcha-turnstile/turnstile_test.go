package turnstile

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }
func configured(t *testing.T) *Captcha {
	t.Helper()
	c := newCaptcha()
	if err := c.ConfigReceiver([]byte(`{"site_key":"public","secret_key":"private&secret","hostnames":"metarhub.com, www.metarhub.com"}`)); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestVerify(t *testing.T) {
	for _, tc := range []struct {
		name, token, body string
		status            int
		network, want     bool
	}{
		{"success", "token+&", `{"success":true,"hostname":"metarhub.com","action":"answer_captcha"}`, 200, false, true},
		{"second host", "token", `{"success":true,"hostname":"www.metarhub.com","action":"answer_captcha"}`, 200, false, true},
		{"missing", "", `{}`, 200, false, false},
		{"long", strings.Repeat("x", 2049), `{}`, 200, false, false},
		{"replay or expired", "token", `{"success":false,"error-codes":["timeout-or-duplicate"]}`, 200, false, false},
		{"wrong host", "token", `{"success":true,"hostname":"metarhub.com.attacker.test","action":"answer_captcha"}`, 200, false, false},
		{"wrong action", "token", `{"success":true,"hostname":"metarhub.com","action":"other"}`, 200, false, false},
		{"missing binding", "token", `{"success":true}`, 200, false, false},
		{"invalid json", "token", `{"success":true`, 200, false, false},
		{"trailing json", "token", `{"success":true,"hostname":"metarhub.com","action":"answer_captcha"} {}`, 200, false, false},
		{"server error", "token", `{"success":true,"hostname":"metarhub.com","action":"answer_captcha"}`, 500, false, false},
		{"oversized response", "token", strings.Repeat("x", 16385), 200, false, false},
		{"timeout", "token", ``, 0, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := configured(t)
			called := false
			c.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
				called = true
				if r.URL.String() != verifyEndpoint || r.Method != "POST" {
					t.Fatalf("unexpected destination: %s", r.URL)
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if r.Form.Get("secret") != "private&secret" || r.Form.Get("response") != tc.token || len(r.Form) != 2 {
					t.Fatal("invalid encoded request")
				}
				if tc.network {
					return nil, errors.New("timeout")
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			if got := c.Verify("", tc.token); got != tc.want {
				t.Fatalf("Verify=%v want=%v", got, tc.want)
			}
			if (tc.token == "" || len(tc.token) > 2048) && called {
				t.Fatal("invalid token sent upstream")
			}
		})
	}
}
func TestConfigAndPublicProjection(t *testing.T) {
	c := configured(t)
	var public map[string]string
	if err := json.Unmarshal([]byte(c.GetConfig()), &public); err != nil {
		t.Fatal(err)
	}
	if len(public) != 2 || public["key"] != "public" || public["action"] != action || strings.Contains(c.GetConfig(), "private") {
		t.Fatal("unsafe public configuration")
	}
	for _, raw := range []string{`{`, `{"site_key":"key"}`, `{"site_key":"key","secret_key":"secret","hostnames":"https://metarhub.com"}`, `{"site_key":"key","secret_key":"secret","hostnames":"*.metarhub.com"}`, `{"site_key":"key","secret_key":"secret","hostnames":"metarhub.com,"}`} {
		if c.ConfigReceiver([]byte(raw)) == nil {
			t.Fatalf("accepted invalid config: %s", raw)
		}
		if c.snapshot().SiteKey != "public" {
			t.Fatal("failed config replaced valid state")
		}
	}
	if err := c.ConfigReceiver([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if c.Verify("", "token") {
		t.Fatal("unconfigured plugin allowed registration")
	}
	if c.client.Timeout <= 0 || c.client.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("unbounded HTTP validation")
	}
}
func TestConcurrentConfig(t *testing.T) {
	c := configured(t)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				_ = c.ConfigReceiver([]byte(`{"site_key":"public","secret_key":"secret","hostnames":"metarhub.com"}`))
				_ = c.GetConfig()
				_ = c.ConfigFields()
			}
		}()
	}
	wg.Wait()
}

func TestVerifyDoesNotCacheOrRetrySuccess(t *testing.T) {
	c := configured(t)
	calls := 0
	c.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"success":false,"error-codes":["timeout-or-duplicate"]}`
		if calls == 1 {
			body = `{"success":true,"hostname":"metarhub.com","action":"answer_captcha"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	if !c.Verify("", "one-token") || c.Verify("", "one-token") || calls != 2 {
		t.Fatal("token replay was accepted or validation retried")
	}
}

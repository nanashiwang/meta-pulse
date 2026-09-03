package pulse_user_center

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestPulseClientSignsCanonicalForumRequest(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	const secret = "pulse-service-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/internal/users/42/profile" {
			t.Fatalf("request=%s %s", r.Method, r.URL.EscapedPath())
		}
		if r.Header.Get(pulseHeaderUserID) != "42" || r.Header.Get(pulseHeaderRole) != forumServiceRole {
			t.Fatalf("identity headers=%+v", r.Header)
		}
		if r.Header.Get(pulseHeaderTimestamp) != strconv.FormatInt(at.Unix(), 10) || r.Header.Get(pulseHeaderNonce) != "nonce-fixed" {
			t.Fatalf("freshness headers=%+v", r.Header)
		}
		canonical := pulseCanonicalPayload(r.Method, r.URL.EscapedPath(), "42", forumServiceRole, at.Unix(), "nonce-fixed", nil)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(canonical))
		if r.Header.Get(pulseHeaderSignature) != hex.EncodeToString(mac.Sum(nil)) {
			t.Fatal("signature does not cover the canonical Pulse request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"user_id":42,"level":{"key":"pulse","name":"脉冲者"},"lifetime_contribution_milli":12000}`)
	}))
	defer server.Close()

	client := NewPulseClient(&Config{PulseBaseURL: server.URL, PulseHMACSecret: secret})
	client.http = server.Client()
	client.now = func() time.Time { return at }
	client.nonce = func() (string, error) { return "nonce-fixed", nil }
	profile, err := client.GetUserProfile("42")
	if err != nil {
		t.Fatal(err)
	}
	if profile.UserID != 42 || profile.Level.Key != "pulse" || profile.Level.Name != "脉冲者" {
		t.Fatalf("profile=%+v", profile)
	}
}

func TestPulseClientFailsClosedWithoutServiceSecret(t *testing.T) {
	client := NewPulseClient(&Config{PulseBaseURL: "https://pulse.example.test"})
	if _, err := client.GetUserProfile("42"); err == nil {
		t.Fatal("profile request succeeded without Pulse service secret")
	}
}

func TestPulseClientRejectsProfileIdentityMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"user_id":99,"level":{"key":"pulse","name":"脉冲者"}}`)
	}))
	defer server.Close()
	client := NewPulseClient(&Config{PulseBaseURL: server.URL, PulseHMACSecret: "secret"})
	client.http = server.Client()
	if _, err := client.GetUserProfile("42"); err == nil {
		t.Fatal("profile response for another user was accepted")
	}
}

func TestUserCenterLoginUsesSSOBridge(t *testing.T) {
	uc := &UserCenter{Config: &Config{NewAPIBaseURL: "https://api.example.test"}}
	description := uc.Description()
	if description.LoginRedirectURL != "https://api.example.test/api/forum/sso/start" {
		t.Fatalf("login redirect=%q", description.LoginRedirectURL)
	}
}

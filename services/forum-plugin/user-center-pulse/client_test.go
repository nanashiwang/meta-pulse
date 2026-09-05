package pulse_user_center

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

func TestUserCenterDoesNotHijackLocalLogin(t *testing.T) {
	uc := &UserCenter{Config: &Config{NewAPIBaseURL: "https://api.example.test"}}
	description := uc.Description()
	if description.LoginRedirectURL != "" || description.SignUpRedirectURL != "" {
		t.Fatalf("local identity was redirected: login=%q signup=%q", description.LoginRedirectURL, description.SignUpRedirectURL)
	}
}

func TestPulseClientRejectsTrailingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"user_id":42,"level":{"key":"pulse","name":"脉冲者"}} {"user_id":43}`)
	}))
	defer server.Close()

	client := NewPulseClient(&Config{PulseBaseURL: server.URL, PulseHMACSecret: "secret"})
	client.http = server.Client()
	if _, err := client.GetUserProfile("42"); err == nil {
		t.Fatal("profile response with trailing JSON was accepted")
	}
}

func TestPulseClientDisablesProxyAndRedirectForwarding(t *testing.T) {
	client := NewPulseClient(&Config{})
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("Pulse client inherited an ambient HTTP proxy")
	}
	if err := client.http.CheckRedirect(&http.Request{}, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy error=%v", err)
	}
}

func TestPulseClientRejectsNonCanonicalExternalIdentity(t *testing.T) {
	client := NewPulseClient(&Config{PulseBaseURL: "http://pulse.internal", PulseHMACSecret: testSecret})
	for _, externalID := range []string{" 42", "042", "+42", "0"} {
		if _, err := client.GetUserProfile(externalID); err == nil {
			t.Fatalf("non-canonical external id %q accepted", externalID)
		}
	}
}

func TestPulseClientAcceptsCanonicalUint64Identity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"user_id":18446744073709551615,"level":{"key":"pulse","name":"脉冲者"},"lifetime_contribution_milli":1}`)
	}))
	defer server.Close()
	client := NewPulseClient(&Config{PulseBaseURL: server.URL, PulseHMACSecret: testSecret})
	profile, err := client.GetUserProfile("18446744073709551615")
	if err != nil || profile.UserID != ^uint64(0) {
		t.Fatalf("profile=%+v err=%v", profile, err)
	}
}

func TestPulseClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"user_id":42,"level":{"key":"pulse","name":"脉冲者"},"padding":"%s"}`, strings.Repeat("x", maxProfileResponseBytes))
	}))
	defer server.Close()

	client := NewPulseClient(&Config{PulseBaseURL: server.URL, PulseHMACSecret: "secret"})
	client.http = server.Client()
	if _, err := client.GetUserProfile("42"); err == nil {
		t.Fatal("oversized profile response was accepted")
	}
}

func TestPulseClientRejectsNegativeContribution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"user_id":42,"level":{"key":"pulse","name":"脉冲者"},"lifetime_contribution_milli":-1}`)
	}))
	defer server.Close()

	client := NewPulseClient(&Config{PulseBaseURL: server.URL, PulseHMACSecret: "secret"})
	client.http = server.Client()
	if _, err := client.GetUserProfile("42"); err == nil {
		t.Fatal("negative profile contribution was accepted")
	}
}

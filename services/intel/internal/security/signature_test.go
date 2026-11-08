package security

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type memNonces map[string]bool

func (m memNonces) ClaimNonce(_ context.Context, n string, _ time.Time) (bool, error) {
	if m[n] {
		return false, nil
	}
	m[n] = true
	return true, nil
}

var (
	secret = []byte(strings.Repeat("s", 32))
	now    = time.Unix(1_800_000_000, 0)
	nonce  = strings.Repeat("ab", 16)
)

func signed(method, path, body, user, role string, ts time.Time, key []byte) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(HeaderUserID, user)
	req.Header.Set(HeaderRole, role)
	req.Header.Set(HeaderTimestamp, strconv.FormatInt(ts.Unix(), 10))
	req.Header.Set(HeaderNonce, nonce)
	req.Header.Set(HeaderSignature, Sign(key, CanonicalPayload(method, path, user, role, ts.Unix(), nonce, []byte(body))))
	return req
}

func TestVerifyAcceptsAndRestoresBody(t *testing.T) {
	req := signed("POST", "/v1/internal/admin/import", `{"a":1}`, "10010000000000001", "admin", now, secret)
	user, body, err := Verify(req, [][]byte{secret}, now, 1024, memNonces{})
	if err != nil || user != "10010000000000001" || string(body) != `{"a":1}` {
		t.Fatalf("verify: %q %q %v", user, body, err)
	}
	again, _ := io.ReadAll(req.Body)
	if string(again) != `{"a":1}` {
		t.Fatal("body must be restored for the handler")
	}
}

func TestVerifyRejects(t *testing.T) {
	other := []byte(strings.Repeat("o", 32))
	cases := map[string]*http.Request{
		"wrong secret": signed("GET", "/v1/internal/admin/overview", "", "1", "admin", now, other),
		"wrong role":   signed("GET", "/v1/internal/admin/overview", "", "1", "user", now, secret),
		"bad user":     signed("GET", "/v1/internal/admin/overview", "", "abc", "admin", now, secret),
		"old":          signed("GET", "/v1/internal/admin/overview", "", "1", "admin", now.Add(-3*time.Minute), secret),
		"future":       signed("GET", "/v1/internal/admin/overview", "", "1", "admin", now.Add(3*time.Minute), secret),
	}
	tampered := signed("POST", "/v1/internal/admin/import", `{"a":1}`, "1", "admin", now, secret)
	tampered.Body = io.NopCloser(strings.NewReader(`{"a":2}`))
	cases["tampered body"] = tampered
	moved := signed("POST", "/v1/internal/admin/import", "", "1", "admin", now, secret)
	moved.URL.Path = "/v1/internal/admin/recompute"
	cases["other path"] = moved
	dup := signed("GET", "/v1/internal/admin/overview", "", "1", "admin", now, secret)
	dup.Header.Add(HeaderUserID, "2")
	cases["duplicate header"] = dup
	for name, req := range cases {
		if _, _, err := Verify(req, [][]byte{secret}, now, 1024, memNonces{}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, _, err := Verify(signed("GET", "/x", "", "1", "admin", now, []byte("short")), [][]byte{[]byte("short")}, now, 1024, memNonces{}); err == nil {
		t.Error("short secret accepted")
	}
	big := signed("POST", "/x", strings.Repeat("x", 2000), "1", "admin", now, secret)
	if _, _, err := Verify(big, [][]byte{secret}, now, 1024, memNonces{}); err == nil {
		t.Error("oversized body accepted")
	}
}

func TestVerifyReplayAndRotation(t *testing.T) {
	nonces := memNonces{}
	old := []byte(strings.Repeat("p", 32))
	if _, _, err := Verify(signed("GET", "/x", "", "1", "admin", now, old), [][]byte{secret, old}, now, 1024, nonces); err != nil {
		t.Fatalf("previous secret rejected: %v", err)
	}
	if _, _, err := Verify(signed("GET", "/x", "", "1", "admin", now, secret), [][]byte{secret}, now, 1024, nonces); err != ErrReplay {
		t.Fatalf("replay not detected: %v", err)
	}
}

package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func signedRequest(method, target, userID, role, nonce, body string, now time.Time, secret []byte) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	timestamp := now.Unix()
	req.Header.Set(HeaderUserID, userID)
	req.Header.Set(HeaderRole, role)
	req.Header.Set(HeaderTimestamp, strconv.FormatInt(timestamp, 10))
	req.Header.Set(HeaderNonce, nonce)
	canonical := CanonicalPayload(method, req.URL.EscapedPath(), userID, role, timestamp, nonce, []byte(body))
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(canonical))
	req.Header.Set(HeaderSignature, hex.EncodeToString(mac.Sum(nil)))
	return req
}

func TestVerifyRequestClaimsNonceAfterSignature(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	secret := []byte("test-secret")
	nonces := NewMemoryNonceStore()
	req := signedRequest(http.MethodPost, "http://pulse/v1/me/pulses", "123", "user-bff", "n-1", `{"ok":true}`, now, secret)

	principal, err := VerifyRequest(req, secret, now, time.Minute, nonces)
	if err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if principal.UserID != 123 || principal.Role != "user-bff" {
		t.Fatalf("unexpected principal: %+v", principal)
	}

	if _, err := VerifyRequest(req, secret, now, time.Minute, nonces); err != ErrReplay {
		t.Fatalf("replayed request error = %v, want %v", err, ErrReplay)
	}
}

func TestVerifyRequestRejectsTamperingAndKeepsNonceAvailable(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	secret := []byte("test-secret")
	nonces := NewMemoryNonceStore()
	req := signedRequest(http.MethodPost, "http://pulse/v1/me/pulses", "123", "user-bff", "n-2", `{"ok":true}`, now, secret)
	req.Body = http.NoBody
	req.Header.Set("X-Body", "tampered")
	if _, err := VerifyRequest(req, secret, now, time.Minute, nonces); err == nil {
		t.Fatal("tampered request accepted")
	}

	legit := signedRequest(http.MethodPost, "http://pulse/v1/me/pulses", "123", "user-bff", "n-2", `{"ok":true}`, now, secret)
	if _, err := VerifyRequest(legit, secret, now, time.Minute, nonces); err != nil {
		t.Fatalf("legitimate request rejected after tampering: %v", err)
	}
}

func TestVerifyRequestRejectsTimeAndMissingSecret(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	secret := []byte("test-secret")
	req := signedRequest(http.MethodGet, "http://pulse/healthz", "123", "service", "n-3", "", now.Add(-2*time.Minute), secret)
	if _, err := VerifyRequest(req, secret, now, time.Minute, NewMemoryNonceStore()); err != ErrTimeWindow {
		t.Fatalf("old request error = %v, want %v", err, ErrTimeWindow)
	}
	if _, err := VerifyRequest(req, nil, now, time.Minute, NewMemoryNonceStore()); err != ErrMissingSecret {
		t.Fatalf("missing secret error = %v, want %v", err, ErrMissingSecret)
	}
}

func TestVerifyRequestAcceptsPreviousSecretDuringRotation(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	current := []byte("current-secret")
	previous := []byte("previous-secret")
	nonces := NewMemoryNonceStore()
	req := signedRequest(http.MethodGet, "http://pulse/v1/me/summary", "123", "user-bff", "n-rotation", "", now, previous)

	if _, err := VerifyRequestWithSecrets(req, [][]byte{current, previous}, now, time.Minute, nonces); err != nil {
		t.Fatalf("request signed by previous secret rejected: %v", err)
	}
}

func TestVerifyRequestDoesNotClaimNonceWhenAllRotationSecretsFail(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	nonces := NewMemoryNonceStore()
	req := signedRequest(http.MethodGet, "http://pulse/v1/me/summary", "123", "user-bff", "n-rotation-invalid", "", now, []byte("old-secret"))

	if _, err := VerifyRequestWithSecrets(req, [][]byte{[]byte("current-secret"), []byte("previous-secret")}, now, time.Minute, nonces); err == nil {
		t.Fatal("request signed by unknown secret was accepted")
	}
	valid := signedRequest(http.MethodGet, "http://pulse/v1/me/summary", "123", "user-bff", "n-rotation-invalid", "", now, []byte("previous-secret"))
	if _, err := VerifyRequestWithSecrets(valid, [][]byte{[]byte("current-secret"), []byte("previous-secret")}, now, time.Minute, nonces); err != nil {
		t.Fatalf("valid retry after failed signature was rejected: %v", err)
	}
}

package transporthttp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/security"
)

func TestSignedRequestMiddlewareAuthenticatesConfiguredRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := []byte("bff-secret")
	now := time.Now().Truncate(time.Second)
	req := httptest.NewRequest(http.MethodGet, "http://pulse/v1/me/summary", nil)
	setSignature(req, now, secret)

	router := gin.New()
	router.Use(SignedRequest(func(role string) []byte {
		if role == "user-bff" {
			return secret
		}
		return nil
	}, &clockNonceStore{now: now}, time.Minute))
	router.GET("/v1/me/summary", func(c *gin.Context) {
		principal, ok := Principal(c)
		if !ok || principal.UserID != 123 {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestSignedRequestMiddlewareFailsClosedForUnknownRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().Truncate(time.Second)
	req := httptest.NewRequest(http.MethodGet, "http://pulse/v1/me/summary", nil)
	setSignature(req, now, []byte("bff-secret"))

	router := gin.New()
	router.Use(SignedRequest(func(string) []byte { return nil }, &clockNonceStore{now: now}, time.Minute))
	router.GET("/v1/me/summary", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

// This test store avoids wall-clock races while testing middleware wiring. The
// production implementation is the Redis SETNX adapter.
type clockNonceStore struct{ now time.Time }

func (s *clockNonceStore) Claim(_ context.Context, _ string, _ time.Time) (bool, error) {
	return true, nil
}

func setSignature(req *http.Request, now time.Time, secret []byte) {
	userID, role, nonce := "123", "user-bff", "middleware-nonce"
	req.Header.Set(security.HeaderUserID, userID)
	req.Header.Set(security.HeaderRole, role)
	req.Header.Set(security.HeaderTimestamp, strconv.FormatInt(now.Unix(), 10))
	req.Header.Set(security.HeaderNonce, nonce)
	canonical := security.CanonicalPayload(req.Method, req.URL.EscapedPath(), userID, role, now.Unix(), nonce, nil)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(canonical))
	req.Header.Set(security.HeaderSignature, hex.EncodeToString(mac.Sum(nil)))
}

func TestSignedRequestWithSecretsAcceptsPreviousSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().Truncate(time.Second)
	previous := []byte("previous-bff-secret")
	req := httptest.NewRequest(http.MethodGet, "http://pulse/v1/me/summary", nil)
	setSignature(req, now, previous)

	router := gin.New()
	router.Use(SignedRequestWithSecrets(func(role string) [][]byte {
		if role == "user-bff" {
			return [][]byte{[]byte("current-bff-secret"), previous}
		}
		return nil
	}, &clockNonceStore{now: now}, time.Minute))
	router.GET("/v1/me/summary", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

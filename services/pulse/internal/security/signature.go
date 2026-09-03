// Package security implements the Pulse internal request signature contract.
// It deliberately does not know anything about Gin or a concrete transport.
package security

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	HeaderUserID    = "X-Pulse-User-Id"
	HeaderRole      = "X-Pulse-Role"
	HeaderTimestamp = "X-Pulse-Timestamp"
	HeaderNonce     = "X-Pulse-Nonce"
	HeaderSignature = "X-Pulse-Signature"
)

var (
	ErrMissingSecret = errors.New("pulse signature secret is not configured")
	ErrInvalid       = errors.New("invalid pulse request signature")
	ErrReplay        = errors.New("replayed pulse request")
	ErrTimeWindow    = errors.New("pulse request is outside the allowed time window")
)

// NonceStore atomically claims a nonce until expiresAt. Implementations may be
// backed by Redis in production; the security verifier itself remains testable
// without Redis.
type NonceStore interface {
	Claim(ctx context.Context, key string, expiresAt time.Time) (bool, error)
}

// MemoryNonceStore is intended for local development and tests only. It is
// process-local and must not be used when multiple API instances are deployed.
type MemoryNonceStore struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func NewMemoryNonceStore() *MemoryNonceStore {
	return &MemoryNonceStore{seen: make(map[string]time.Time)}
}

func (s *MemoryNonceStore) Claim(ctx context.Context, key string, expiresAt time.Time) (bool, error) {
	if ctx == nil {
		return false, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s == nil {
		return false, errors.New("nil nonce store")
	}
	if key == "" {
		return false, errors.New("empty nonce")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for existing, expiry := range s.seen {
		if !expiry.After(now) {
			delete(s.seen, existing)
		}
	}
	if expiry, ok := s.seen[key]; ok && expiry.After(now) {
		return false, nil
	}
	s.seen[key] = expiresAt
	return true, nil
}

// Principal is the authenticated identity carried by an internal request.
type Principal struct {
	UserID uint64
	Role   string
	Nonce  string
}

// VerifyRequest validates the canonical HMAC contract and claims the nonce only
// after all cryptographic and timestamp checks succeed.
func VerifyRequest(req *http.Request, secret []byte, now time.Time, allowedSkew time.Duration, nonces NonceStore) (Principal, error) {
	if len(secret) == 0 {
		return Principal{}, ErrMissingSecret
	}
	if req == nil || req.URL == nil {
		return Principal{}, ErrInvalid
	}

	userID, err := strconv.ParseUint(strings.TrimSpace(req.Header.Get(HeaderUserID)), 10, 64)
	if err != nil || userID == 0 {
		return Principal{}, fmt.Errorf("%w: invalid user id", ErrInvalid)
	}
	role := strings.TrimSpace(req.Header.Get(HeaderRole))
	nonce := strings.TrimSpace(req.Header.Get(HeaderNonce))
	if role == "" || nonce == "" {
		return Principal{}, fmt.Errorf("%w: missing role or nonce", ErrInvalid)
	}

	timestamp, err := strconv.ParseInt(strings.TrimSpace(req.Header.Get(HeaderTimestamp)), 10, 64)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: invalid timestamp", ErrInvalid)
	}
	requestTime := time.Unix(timestamp, 0)
	if allowedSkew <= 0 {
		allowedSkew = 5 * time.Minute
	}
	if delta := now.Sub(requestTime); delta > allowedSkew || delta < -allowedSkew {
		return Principal{}, ErrTimeWindow
	}

	var body []byte
	if req.Body != nil {
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return Principal{}, fmt.Errorf("%w: read body: %v", ErrInvalid, err)
		}
	}
	// Preserve the body for the actual handler.
	req.Body = io.NopCloser(bytes.NewReader(body))

	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := CanonicalPayload(req.Method, path, strconv.FormatUint(userID, 10), role, timestamp, nonce, body)
	provided, err := hex.DecodeString(strings.TrimSpace(req.Header.Get(HeaderSignature)))
	if err != nil || len(provided) != sha256.Size {
		return Principal{}, fmt.Errorf("%w: malformed signature", ErrInvalid)
	}

	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(canonical))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return Principal{}, ErrInvalid
	}
	if nonces == nil {
		return Principal{}, errors.New("nonce store is not configured")
	}

	claimed, err := nonces.Claim(req.Context(), nonceKey(role, strconv.FormatUint(userID, 10), nonce), requestTime.Add(allowedSkew))
	if err != nil {
		return Principal{}, fmt.Errorf("claim nonce: %w", err)
	}
	if !claimed {
		return Principal{}, ErrReplay
	}
	return Principal{UserID: userID, Role: role, Nonce: nonce}, nil
}

// CanonicalPayload is public so new-api, forum adapters, and tests can share
// exactly the same serialization. bodyHash is SHA-256 over the raw request
// body, represented as lowercase hexadecimal.
func CanonicalPayload(method, path, userID, role string, timestamp int64, nonce string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	return strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(method)),
		path,
		userID,
		role,
		strconv.FormatInt(timestamp, 10),
		nonce,
		hex.EncodeToString(bodyHash[:]),
	}, "\n")
}

func nonceKey(role, userID, nonce string) string {
	return role + ":" + userID + ":" + nonce
}

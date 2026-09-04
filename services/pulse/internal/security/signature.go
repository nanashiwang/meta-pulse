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
	// MaxSignedRequestBodyBytes keeps the authentication boundary from reading
	// an attacker-controlled body without a bound. It is aligned with the
	// new-api Pulse service-auth limit.
	MaxSignedRequestBodyBytes = 64 << 10
	MaxSignedRoleBytes        = 64
	MaxSignedNonceBytes       = 128

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
	return VerifyRequestWithSecrets(req, [][]byte{secret}, now, allowedSkew, nonces)
}

// VerifyRequestWithSecrets accepts the active secret followed by an optional
// previous secret during an intentional rotation window. The nonce is claimed
// only after one of the supplied keys authenticates the complete request.
func VerifyRequestWithSecrets(req *http.Request, secrets [][]byte, now time.Time, allowedSkew time.Duration, nonces NonceStore) (Principal, error) {
	configured := false
	for _, secret := range secrets {
		if len(secret) > 0 {
			configured = true
			break
		}
	}
	if !configured {
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
	if len(role) > MaxSignedRoleBytes || len(nonce) > MaxSignedNonceBytes {
		return Principal{}, fmt.Errorf("%w: role or nonce is too long", ErrInvalid)
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
		body, err = io.ReadAll(io.LimitReader(req.Body, MaxSignedRequestBodyBytes+1))
		if err != nil {
			return Principal{}, fmt.Errorf("%w: read body: %v", ErrInvalid, err)
		}
		if len(body) > MaxSignedRequestBodyBytes {
			return Principal{}, fmt.Errorf("%w: request body exceeds %d bytes", ErrInvalid, MaxSignedRequestBodyBytes)
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

	matched := false
	for _, secret := range secrets {
		if len(secret) == 0 {
			continue
		}
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write([]byte(canonical))
		if hmac.Equal(provided, mac.Sum(nil)) {
			matched = true
			break
		}
	}
	if !matched {
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

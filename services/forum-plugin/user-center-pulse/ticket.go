package pulse_user_center

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ticketTTL bounds how long a login ticket stays valid. Long enough to survive
// a redirect, short enough that a leaked URL in browser history or a referrer
// header is useless.
const ticketTTL = 2 * time.Minute

// LoginTicketNonceStore atomically claims a signed ticket nonce. Production
// implementations must be shared by every Answer instance.
type LoginTicketNonceStore interface {
	Claim(ctx context.Context, nonce string, expiresAt time.Time) (bool, error)
}

// LoginTicket is the only trusted way for the browser to assert an identity.
//
// The user center callback is reached by a browser redirect, not a server-to-
// server call. Without a signature, anyone could hit the callback with
// ?user_id=1 and become that user. new-api mints a ticket, signs it, and the
// browser can only relay it — it cannot forge one.
//
// AGENTS.md invariant 13: the browser must never assert a trusted user_id.
type LoginTicket struct {
	UserID      string
	Username    string
	DisplayName string
	Email       string
	Avatar      string
	Timestamp   int64
	Nonce       string
	Signature   string
}

// signingPayload builds the string that new-api signs and we verify.
//
// Fields are newline-joined using the contract shared with new-api. Verify must
// reject CR/LF in every string field before checking the signature; otherwise a
// separator embedded in one field could shift data into an adjacent field.
func (t *LoginTicket) signingPayload() string {
	return strings.Join([]string{
		t.UserID,
		t.Username,
		t.DisplayName,
		t.Email,
		t.Avatar,
		strconv.FormatInt(t.Timestamp, 10),
		t.Nonce,
	}, "\n")
}

func (t *LoginTicket) validateSignedFields() error {
	userID, err := strconv.ParseUint(t.UserID, 10, 64)
	if err != nil || userID == 0 || strconv.FormatUint(userID, 10) != t.UserID {
		return fmt.Errorf("invalid user id in login ticket")
	}
	for _, field := range []string{t.UserID, t.Username, t.DisplayName, t.Email, t.Avatar, t.Nonce} {
		if strings.ContainsAny(field, "\r\n") {
			return fmt.Errorf("login ticket fields must not contain CR or LF")
		}
	}
	return nil
}

// Verify checks the ticket signature, freshness, and single use.
func (t *LoginTicket) Verify(ctx context.Context, secret string, nonces LoginTicketNonceStore, now time.Time) error {
	return t.VerifyWithSecrets(ctx, []string{secret}, nonces, now)
}

// VerifyWithSecrets accepts the active SSO secret followed by an optional
// previous secret during a controlled rotation window. The nonce is consumed
// only after the ticket authenticates with one of the configured keys.
func (t *LoginTicket) VerifyWithSecrets(ctx context.Context, secrets []string, nonces LoginTicketNonceStore, now time.Time) error {
	configured := false
	for _, secret := range secrets {
		if strings.TrimSpace(secret) != "" {
			configured = true
			break
		}
	}
	if !configured {
		return fmt.Errorf("hmac secret not configured")
	}
	if t.UserID == "" || t.Nonce == "" || t.Signature == "" {
		return fmt.Errorf("incomplete login ticket")
	}
	if err := t.validateSignedFields(); err != nil {
		return err
	}
	if nonces == nil {
		return fmt.Errorf("login ticket nonce store not configured")
	}
	if ctx == nil {
		return fmt.Errorf("login ticket context not configured")
	}

	// Reject stale tickets in both directions: an old ticket may have leaked,
	// and a far-future one indicates a forged or clock-skewed timestamp.
	issuedAt := time.Unix(t.Timestamp, 0)
	age := now.Sub(issuedAt)
	if age > ticketTTL || age < -ticketTTL {
		return fmt.Errorf("login ticket expired or not yet valid")
	}

	got, err := hex.DecodeString(t.Signature)
	if err != nil {
		return fmt.Errorf("malformed login ticket signature")
	}
	matched := false
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			continue
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(t.signingPayload()))
		if hmac.Equal(mac.Sum(nil), got) {
			matched = true
			break
		}
	}
	if !matched {
		return fmt.Errorf("invalid login ticket signature")
	}

	unused, err := nonces.Claim(ctx, t.Nonce, issuedAt.Add(ticketTTL))
	if err != nil {
		return fmt.Errorf("login ticket nonce store unavailable: %w", err)
	}
	if !unused {
		return fmt.Errorf("login ticket already used")
	}
	return nil
}

// NonceCache is a process-local implementation for unit tests only. Production
// plugin setup always replaces it with RedisNonceStore.
type NonceCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
	now  func() time.Time
}

func NewNonceCache() *NonceCache {
	return &NonceCache{seen: make(map[string]time.Time), now: time.Now}
}

func (c *NonceCache) Claim(ctx context.Context, nonce string, expiresAt time.Time) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("nil context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if c == nil || nonce == "" || c.now == nil || !expiresAt.After(c.now()) {
		return false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for key, expiry := range c.seen {
		if !expiry.After(now) {
			delete(c.seen, key)
		}
	}
	if expiry, exists := c.seen[nonce]; exists && expiry.After(now) {
		return false, nil
	}
	c.seen[nonce] = expiresAt
	return true, nil
}

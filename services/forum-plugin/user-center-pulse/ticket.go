package pulse_user_center

import (
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
// Fields are newline-joined rather than concatenated so that a value containing
// the separator cannot be shifted into an adjacent field: signing "a|b" + "c"
// and "a" + "b|c" must not produce the same payload.
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

// Verify checks the ticket signature, freshness, and single use.
func (t *LoginTicket) Verify(secret string, seen *NonceCache, now time.Time) error {
	if secret == "" {
		return fmt.Errorf("hmac secret not configured")
	}
	if t.UserID == "" || t.Nonce == "" || t.Signature == "" {
		return fmt.Errorf("incomplete login ticket")
	}
	if _, err := strconv.ParseInt(t.UserID, 10, 64); err != nil {
		return fmt.Errorf("invalid user id in login ticket")
	}

	// Reject stale tickets in both directions: an old ticket may have leaked,
	// and a far-future one indicates a forged or clock-skewed timestamp.
	age := now.Sub(time.Unix(t.Timestamp, 0))
	if age > ticketTTL || age < -ticketTTL {
		return fmt.Errorf("login ticket expired or not yet valid")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(t.signingPayload()))
	expected := mac.Sum(nil)

	got, err := hex.DecodeString(t.Signature)
	if err != nil {
		return fmt.Errorf("malformed login ticket signature")
	}
	// Constant-time compare: a byte-by-byte comparison leaks how much of the
	// signature matched, which is enough to forge one byte at a time.
	if !hmac.Equal(expected, got) {
		return fmt.Errorf("invalid login ticket signature")
	}

	// Signature verified; now burn the nonce so a captured URL cannot be
	// replayed within its TTL window.
	if !seen.Use(t.Nonce, now) {
		return fmt.Errorf("login ticket already used")
	}
	return nil
}

// NonceCache makes a verified ticket single-use.
//
// Entries are kept for ticketTTL only; after that the timestamp check rejects
// the ticket anyway, so remembering it longer buys nothing and leaks memory.
type NonceCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func NewNonceCache() *NonceCache {
	return &NonceCache{seen: make(map[string]time.Time)}
}

// Use records a nonce and reports whether it was previously unused.
func (c *NonceCache) Use(nonce string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, at := range c.seen {
		if now.Sub(at) > ticketTTL {
			delete(c.seen, key)
		}
	}

	if _, exists := c.seen[nonce]; exists {
		return false
	}
	c.seen[nonce] = now
	return true
}

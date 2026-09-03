package pulse_user_center

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret"

// mintTicket produces a ticket the way new-api is expected to.
func mintTicket(userID string, at time.Time, nonce string) *LoginTicket {
	t := &LoginTicket{
		UserID:      userID,
		Username:    "alice",
		DisplayName: "Alice",
		Email:       "alice@example.test",
		Avatar:      "",
		Timestamp:   at.Unix(),
		Nonce:       nonce,
	}
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte(t.signingPayload()))
	t.Signature = hex.EncodeToString(mac.Sum(nil))
	return t
}

func TestValidTicketAccepted(t *testing.T) {
	now := time.Now()
	ticket := mintTicket("123", now, "nonce-1")

	if err := ticket.Verify(context.Background(), testSecret, NewNonceCache(), now); err != nil {
		t.Fatalf("valid ticket rejected: %v", err)
	}
}

// The core vulnerability class: the callback is a browser redirect, so an
// attacker fully controls the query string. Without a signature check, any of
// these would log the attacker in as user 1.
func TestForgedTicketRejected(t *testing.T) {
	now := time.Now()

	for _, tc := range []struct {
		name   string
		mutate func(*LoginTicket)
	}{
		{"no signature", func(t *LoginTicket) { t.Signature = "" }},
		{"garbage signature", func(t *LoginTicket) { t.Signature = "deadbeef" }},
		{"non-hex signature", func(t *LoginTicket) { t.Signature = "zzzz" }},
		{"escalate user id", func(t *LoginTicket) { t.UserID = "1" }},
		{"swap email", func(t *LoginTicket) { t.Email = "admin@example.test" }},
		{"non-numeric user id", func(t *LoginTicket) { t.UserID = "admin" }},
		{"empty user id", func(t *LoginTicket) { t.UserID = "" }},
		{"missing nonce", func(t *LoginTicket) { t.Nonce = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ticket := mintTicket("123", now, "nonce-"+tc.name)
			tc.mutate(ticket)

			if err := ticket.Verify(context.Background(), testSecret, NewNonceCache(), now); err == nil {
				t.Error("forged ticket accepted")
			}
		})
	}
}

func TestSignedNonCanonicalUserIDRejected(t *testing.T) {
	now := time.Now()
	for _, userID := range []string{"0", "-1", "+1", "001"} {
		t.Run(userID, func(t *testing.T) {
			ticket := mintTicket(userID, now, "nonce-user-"+userID)
			if err := ticket.Verify(context.Background(), testSecret, NewNonceCache(), now); err == nil {
				t.Fatalf("signed non-canonical user id %q was accepted", userID)
			}
		})
	}
}

func TestSignedFieldsRejectCRLFBoundaryShifting(t *testing.T) {
	now := time.Now()
	a := &LoginTicket{
		UserID: "1", Username: "alice\nAdmin", DisplayName: "X", Email: "Y", Avatar: "Z",
		Timestamp: now.Unix(), Nonce: "nonce-boundary",
	}
	b := &LoginTicket{
		UserID: "1", Username: "alice", DisplayName: "Admin", Email: "X", Avatar: "Y\nZ",
		Timestamp: now.Unix(), Nonce: "nonce-boundary",
	}
	if a.signingPayload() != b.signingPayload() {
		t.Fatal("test setup does not reproduce the newline boundary ambiguity")
	}
	mac := hmac.New(sha256.New, []byte(testSecret))
	_, _ = mac.Write([]byte(a.signingPayload()))
	signature := hex.EncodeToString(mac.Sum(nil))
	a.Signature, b.Signature = signature, signature
	for _, ticket := range []*LoginTicket{a, b} {
		if err := ticket.Verify(context.Background(), testSecret, NewNonceCache(), now); err == nil {
			t.Fatal("correctly signed ticket containing a newline was accepted")
		}
	}
}

func TestWrongSecretRejected(t *testing.T) {
	now := time.Now()
	ticket := mintTicket("123", now, "nonce-secret")

	if err := ticket.Verify(context.Background(), "different-secret", NewNonceCache(), now); err == nil {
		t.Error("ticket signed with a different secret was accepted")
	}
}

// An unconfigured secret must fail closed. Empty-secret HMAC is still a valid
// HMAC, so without this check an attacker who knows the secret is unset could
// mint their own tickets.
func TestEmptySecretRejected(t *testing.T) {
	now := time.Now()
	ticket := &LoginTicket{UserID: "123", Timestamp: now.Unix(), Nonce: "n", Signature: "ab"}

	if err := ticket.Verify(context.Background(), "", NewNonceCache(), now); err == nil {
		t.Error("verification succeeded with no secret configured")
	}
}

func TestExpiredTicketRejected(t *testing.T) {
	now := time.Now()

	stale := mintTicket("123", now.Add(-ticketTTL-time.Second), "nonce-old")
	if err := stale.Verify(context.Background(), testSecret, NewNonceCache(), now); err == nil {
		t.Error("expired ticket accepted")
	}

	// A future timestamp means a forged or badly skewed clock.
	future := mintTicket("123", now.Add(ticketTTL+time.Second), "nonce-future")
	if err := future.Verify(context.Background(), testSecret, NewNonceCache(), now); err == nil {
		t.Error("future-dated ticket accepted")
	}
}

// A ticket lands in browser history, referrer headers, and proxy logs. Even
// with a valid signature it must work exactly once.
func TestTicketIsSingleUse(t *testing.T) {
	now := time.Now()
	nonces := NewNonceCache()
	ticket := mintTicket("123", now, "nonce-replay")

	if err := ticket.Verify(context.Background(), testSecret, nonces, now); err != nil {
		t.Fatalf("first use rejected: %v", err)
	}
	if err := ticket.Verify(context.Background(), testSecret, nonces, now); err == nil {
		t.Error("replayed ticket accepted")
	}
}

// A failed verification must not burn the nonce, or an attacker could grief a
// user by racing a forged ticket with their nonce before they use it.
func TestFailedVerificationDoesNotBurnNonce(t *testing.T) {
	now := time.Now()
	nonces := NewNonceCache()

	forged := mintTicket("123", now, "nonce-shared")
	forged.Signature = "deadbeef"
	if err := forged.Verify(context.Background(), testSecret, nonces, now); err == nil {
		t.Fatal("forged ticket accepted")
	}

	legit := mintTicket("123", now, "nonce-shared")
	if err := legit.Verify(context.Background(), testSecret, nonces, now); err != nil {
		t.Errorf("legitimate ticket rejected after a forgery attempt: %v", err)
	}
}

func TestNonceCacheEvictsExpiredEntries(t *testing.T) {
	now := time.Now()
	nonces := NewNonceCache()
	nonces.now = func() time.Time { return now }
	if claimed, err := nonces.Claim(context.Background(), "old", now.Add(ticketTTL)); err != nil || !claimed {
		t.Fatalf("claim old: claimed=%v err=%v", claimed, err)
	}

	// Well past the TTL, the entry is dropped. The timestamp check is what
	// actually rejects such a ticket; this only bounds memory.
	now = now.Add(2 * ticketTTL)
	if claimed, err := nonces.Claim(context.Background(), "trigger-sweep", now.Add(ticketTTL)); err != nil || !claimed {
		t.Fatalf("claim trigger: claimed=%v err=%v", claimed, err)
	}

	if len(nonces.seen) != 1 {
		t.Errorf("cache holds %d entries, want 1 after eviction", len(nonces.seen))
	}
}

func TestNonceCacheIsConcurrencySafe(t *testing.T) {
	now := time.Now()
	nonces := NewNonceCache()
	nonces.now = func() time.Time { return now }

	type claimResult struct {
		claimed bool
		err     error
	}
	const goroutines = 50
	results := make(chan claimResult, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			claimed, err := nonces.Claim(context.Background(), "contested", now.Add(ticketTTL))
			results <- claimResult{claimed: claimed, err: err}
		}()
	}

	accepted := 0
	for i := 0; i < goroutines; i++ {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.claimed {
			accepted++
		}
	}
	// Exactly one concurrent caller may win, or double-spend is possible.
	if accepted != 1 {
		t.Errorf("%d goroutines claimed the same nonce, want exactly 1", accepted)
	}
}

func TestSigningPayloadFieldCount(t *testing.T) {
	ticket := mintTicket("123", time.Now(), "n")
	// Guards against silently dropping a field from the signature: an unsigned
	// field is attacker-controlled.
	if got := strings.Count(ticket.signingPayload(), "\n"); got != 6 {
		t.Errorf("payload has %d separators, want 6 (7 signed fields)", got)
	}
	if !strings.Contains(ticket.signingPayload(), strconv.FormatInt(ticket.Timestamp, 10)) {
		t.Error("timestamp is not covered by the signature")
	}
}

type failingNonceStore struct{}

func (failingNonceStore) Claim(context.Context, string, time.Time) (bool, error) {
	return false, errors.New("redis unavailable")
}

func TestTicketFailsClosedWhenNonceStoreUnavailable(t *testing.T) {
	now := time.Now()
	ticket := mintTicket("123", now, "nonce-store-down")
	if err := ticket.Verify(context.Background(), testSecret, failingNonceStore{}, now); err == nil {
		t.Fatal("ticket was accepted while the shared nonce store was unavailable")
	}
	if err := ticket.Verify(context.Background(), testSecret, nil, now); err == nil {
		t.Fatal("ticket was accepted without a shared nonce store")
	}
}

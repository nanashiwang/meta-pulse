package pulse_user_center

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/apache/answer/plugin"
)

// The plugin is registered via plugin.Register(&UserCenter{}) in init, which
// accepts plugin.Base. That means a missing UserCenter method is only caught at
// runtime when Answer type-asserts the plugin. These assertions move the check
// to compile time.
var (
	_ plugin.Base       = (*UserCenter)(nil)
	_ plugin.UserCenter = (*UserCenter)(nil)
	_ plugin.Config     = (*UserCenter)(nil)
)

// Invariant: the forum must never own identity or govern its own moderation
// ranks off the back of paid spend. See docs/COMMUNITY.md.
func TestDescriptionEnforcesIdentityBoundaries(t *testing.T) {
	uc := &UserCenter{Config: &Config{NewAPIBaseURL: "https://example.test"}}
	desc := uc.Description()

	if desc.EnabledOriginalUserSystem {
		t.Error("forum must not allow independent registration; identity belongs to new-api")
	}
	if desc.RankAgentEnabled {
		t.Error("rank agent must stay off; Answer rank gates moderation privileges")
	}
	if desc.UserRoleAgentEnabled {
		t.Error("role agent must stay off; paid spend must not grant forum roles")
	}
}

// The callback is a browser redirect, so an unsigned request must never
// authenticate anyone. Before signature verification existed, the first case
// here logged the attacker in as user 1.
func TestResolveUserRejectsUnsignedCallback(t *testing.T) {
	uc := &UserCenter{Config: &Config{HMACSecret: testSecret}, Nonces: NewNonceCache()}

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"forged admin", "?user_id=1&username=admin&timestamp=0&nonce=n"},
		{"no signature", "?user_id=123&timestamp=0&nonce=n"},
		{"no timestamp", "?user_id=123&nonce=n&signature=ab"},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := uc.resolveUser(newTestContext(tc.query)); err == nil {
				t.Errorf("unsigned callback %q authenticated a user", tc.query)
			}
		})
	}
}

func TestResolveUserAcceptsSignedTicket(t *testing.T) {
	uc := &UserCenter{Config: &Config{HMACSecret: testSecret}, Nonces: NewNonceCache()}
	ticket := mintTicket("123", time.Now(), "nonce-resolve")

	query := url.Values{
		"user_id":      {ticket.UserID},
		"username":     {ticket.Username},
		"display_name": {ticket.DisplayName},
		"email":        {ticket.Email},
		"avatar":       {ticket.Avatar},
		"timestamp":    {strconv.FormatInt(ticket.Timestamp, 10)},
		"nonce":        {ticket.Nonce},
		"signature":    {ticket.Signature},
	}

	info, err := uc.resolveUser(newTestContext("?" + query.Encode()))
	if err != nil {
		t.Fatalf("signed ticket rejected: %v", err)
	}
	if info.ExternalID != "123" {
		t.Errorf("external id = %q, want 123", info.ExternalID)
	}
}

// Answer requires a display name; fall back to username rather than blank.
func TestResolveUserFallsBackToUsername(t *testing.T) {
	uc := &UserCenter{Config: &Config{HMACSecret: testSecret}, Nonces: NewNonceCache()}
	ticket := mintTicket("123", time.Now(), "nonce-fallback")
	ticket.DisplayName = ""
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte(ticket.signingPayload()))
	ticket.Signature = hex.EncodeToString(mac.Sum(nil))

	query := url.Values{
		"user_id":   {ticket.UserID},
		"username":  {ticket.Username},
		"email":     {ticket.Email},
		"timestamp": {strconv.FormatInt(ticket.Timestamp, 10)},
		"nonce":     {ticket.Nonce},
		"signature": {ticket.Signature},
	}

	info, err := uc.resolveUser(newTestContext("?" + query.Encode()))
	if err != nil {
		t.Fatalf("signed ticket rejected: %v", err)
	}
	if info.DisplayName != "alice" {
		t.Errorf("display name = %q, want alice fallback", info.DisplayName)
	}
}

// A Pulse outage must degrade to "no badge", never to a locked account or a
// failed login. Nil client makes every call fail, simulating total outage.
func TestPulseOutageDegradesGracefully(t *testing.T) {
	uc := &UserCenter{
		Config: &Config{LevelBadgeEnabled: true},
		Client: NewPulseClient(&Config{}),
	}

	if status := uc.UserStatus("123"); status != plugin.UserStatusAvailable {
		t.Errorf("status = %v, want available when Pulse is down", status)
	}
	if branding := uc.PersonalBranding("123"); branding != nil {
		t.Errorf("branding = %v, want nil when Pulse is down", branding)
	}
}

func TestFormatContribution(t *testing.T) {
	// 1 contribution = 1000 contribution_milli (AGENTS.md section 7).
	for _, tc := range []struct {
		milli int64
		want  string
	}{
		{0, "0"},
		{999, "0"},
		{1000, "1"},
		{1500, "1"},
		{1234567, "1234"},
	} {
		if got := formatContribution(tc.milli); got != tc.want {
			t.Errorf("formatContribution(%d) = %q, want %q", tc.milli, got, tc.want)
		}
	}
}

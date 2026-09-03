package pulse_user_center

import (
	"testing"

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

func TestResolveUserRejectsUntrustedUserID(t *testing.T) {
	uc := &UserCenter{Config: &Config{}}

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"missing", ""},
		{"non-numeric", "?user_id=admin"},
		{"injection", "?user_id=1%20OR%201"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newTestContext(tc.query)
			if _, err := uc.resolveUser(ctx); err == nil {
				t.Errorf("expected rejection for %q", tc.query)
			}
		})
	}
}

func TestResolveUserMapsTrustedHeaders(t *testing.T) {
	uc := &UserCenter{Config: &Config{}}
	ctx := newTestContext("?user_id=123&username=alice&email=alice@example.test")

	info, err := uc.resolveUser(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ExternalID != "123" {
		t.Errorf("external id = %q, want 123", info.ExternalID)
	}
	// Answer requires a display name; fall back to username rather than blank.
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

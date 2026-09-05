package pulse_user_center

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/apache/answer/plugin"
)

var (
	_ plugin.Base       = (*UserCenter)(nil)
	_ plugin.UserCenter = (*UserCenter)(nil)
	_ plugin.Connector  = (*UserCenter)(nil)
	_ plugin.Config     = (*UserCenter)(nil)
)

type memoryLoginStore struct {
	mu     sync.Mutex
	flows  map[string]time.Time
	nonces map[string]time.Time
	err    error
}

func newMemoryLoginStore() *memoryLoginStore {
	return &memoryLoginStore{flows: map[string]time.Time{}, nonces: map[string]time.Time{}}
}

func (s *memoryLoginStore) Begin(ctx context.Context, flowID string, expiresAt time.Time) error {
	if s.err != nil {
		return s.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.flows[flowID]; exists {
		return errors.New("duplicate flow")
	}
	s.flows[flowID] = expiresAt
	return nil
}

func (s *memoryLoginStore) Consume(ctx context.Context, flowID, nonce string, expiresAt time.Time) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	flowExpiry, exists := s.flows[flowID]
	if !exists || !flowExpiry.After(time.Now()) || !expiresAt.After(time.Now()) {
		return false, nil
	}
	if nonceExpiry, exists := s.nonces[nonce]; exists && nonceExpiry.After(time.Now()) {
		return false, nil
	}
	delete(s.flows, flowID)
	s.nonces[nonce] = expiresAt
	return true, nil
}

type fakeBindingGuard struct{ err error }

func (g *fakeBindingGuard) Ensure(context.Context) error { return g.err }
func (g *fakeBindingGuard) Ready(context.Context) error  { return g.err }
func (g *fakeBindingGuard) Close() error                 { return nil }

func TestDescriptionKeepsCommunityIdentityLocal(t *testing.T) {
	uc := &UserCenter{Config: &Config{NewAPIBaseURL: "https://example.test"}}
	desc := uc.Description()
	if !desc.EnabledOriginalUserSystem {
		t.Error("Answer local registration must remain enabled")
	}
	if desc.LoginRedirectURL != "" || desc.SignUpRedirectURL != "" {
		t.Fatal("local login or sign-up was redirected to new-api")
	}
	if desc.UserStatusAgentEnabled {
		t.Error("new-api/Pulse status must not override Answer local bans")
	}
	if desc.RankAgentEnabled || desc.UserRoleAgentEnabled {
		t.Error("paid usage must not grant forum governance permissions")
	}
	settings, err := uc.UserSettings("123")
	if err != nil || settings.ProfileSettingRedirectURL != "" || settings.AccountSettingRedirectURL != "" {
		t.Fatalf("local settings were redirected: settings=%+v err=%v", settings, err)
	}
}

func TestConnectorSenderCreatesSecureBrowserFlow(t *testing.T) {
	store := newMemoryLoginStore()
	uc := &UserCenter{Config: &Config{NewAPIBaseURL: "https://api.example.test"}, Logins: store}
	ctx := newTestContext("")
	if got := uc.ConnectorSender(ctx, "https://forum.example.test/answer/api/v1/connector/redirect/pulse_user_center"); got != "https://api.example.test/api/forum/sso/start" {
		t.Fatalf("redirect=%q", got)
	}
	cookies := ctx.Writer.Header().Values("Set-Cookie")
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie=%v", cookies)
	}
	response := &http.Response{Header: ctx.Writer.Header()}
	parsed := response.Cookies()
	if len(parsed) != 1 || parsed[0].Name != forumLoginFlowCookie || !parsed[0].Secure || !parsed[0].HttpOnly || parsed[0].SameSite != http.SameSiteLaxMode || parsed[0].Path != forumCallbackPath {
		t.Fatalf("unsafe flow cookie: %+v", parsed)
	}
	if len(store.flows) != 1 {
		t.Fatalf("stored flows=%d", len(store.flows))
	}
}

func TestConnectorReceiverRequiresSignedBrowserBoundTicket(t *testing.T) {
	store := newMemoryLoginStore()
	guard := &fakeBindingGuard{}
	uc := &UserCenter{Config: &Config{SSOHMACSecret: testSecret}, Logins: store, Guard: guard}
	ticket := mintTicket("123", time.Now(), "nonce-connector")

	// A valid callback URL copied into a browser that never started the flow is
	// rejected without spending the ticket nonce.
	ctx := newTestContext("?" + ticketQuery(ticket).Encode())
	if _, err := uc.ConnectorReceiver(ctx, ""); err == nil {
		t.Fatal("unsolicited callback was accepted")
	}

	flowID := testFlowID("legitimate")
	if err := store.Begin(context.Background(), flowID, time.Now().Add(loginFlowTTL)); err != nil {
		t.Fatal(err)
	}
	ctx = newTestContext("?" + ticketQuery(ticket).Encode())
	ctx.Request.AddCookie(&http.Cookie{Name: forumLoginFlowCookie, Value: flowID})
	info, err := uc.ConnectorReceiver(ctx, "")
	if err != nil {
		t.Fatalf("signed callback rejected: %v", err)
	}
	if info.ExternalID != "123" || info.DisplayName != "Alice" || info.Email != "" || info.Avatar != "" || info.MetaInfo != "" {
		t.Fatalf("unsafe connector projection: %+v", info)
	}

	secondFlow := testFlowID("replay")
	_ = store.Begin(context.Background(), secondFlow, time.Now().Add(loginFlowTTL))
	ctx = newTestContext("?" + ticketQuery(ticket).Encode())
	ctx.Request.AddCookie(&http.Cookie{Name: forumLoginFlowCookie, Value: secondFlow})
	if _, err := uc.ConnectorReceiver(ctx, ""); err == nil {
		t.Fatal("replayed ticket was accepted through a new browser flow")
	}
}

func TestConnectorReceiverFailsClosedBeforeSpendingTicket(t *testing.T) {
	store := newMemoryLoginStore()
	guard := &fakeBindingGuard{err: errors.New("trigger missing")}
	uc := &UserCenter{Config: &Config{SSOHMACSecret: testSecret}, Logins: store, Guard: guard}
	ticket := mintTicket("123", time.Now(), "nonce-guard")
	flowID := testFlowID("guard")
	_ = store.Begin(context.Background(), flowID, time.Now().Add(loginFlowTTL))
	ctx := newTestContext("?" + ticketQuery(ticket).Encode())
	ctx.Request.AddCookie(&http.Cookie{Name: forumLoginFlowCookie, Value: flowID})
	if _, err := uc.ConnectorReceiver(ctx, ""); err == nil {
		t.Fatal("callback was accepted without database binding invariants")
	}

	guard.err = nil
	ctx = newTestContext("?" + ticketQuery(ticket).Encode())
	ctx.Request.AddCookie(&http.Cookie{Name: forumLoginFlowCookie, Value: flowID})
	if _, err := uc.ConnectorReceiver(ctx, ""); err != nil {
		t.Fatalf("ticket was burned before binding guard recovered: %v", err)
	}
}

func TestConnectorReceiverRejectsAmbiguousQueries(t *testing.T) {
	base := mintTicket("123", time.Now(), "nonce-query")
	for _, tc := range []struct {
		name   string
		mutate func(url.Values)
	}{
		{"duplicate user", func(q url.Values) { q.Add("user_id", "456") }},
		{"unexpected field", func(q url.Values) { q.Set("next", "https://evil.test") }},
		{"missing field", func(q url.Values) { q.Del("email") }},
		{"non-canonical timestamp", func(q url.Values) { q.Set("timestamp", "+"+q.Get("timestamp")) }},
		{"zero timestamp", func(q url.Values) { q.Set("timestamp", "0") }},
		{"forged signature", func(q url.Values) { q.Set("signature", "deadbeef") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryLoginStore()
			flowID := testFlowID(tc.name)
			_ = store.Begin(context.Background(), flowID, time.Now().Add(loginFlowTTL))
			query := ticketQuery(base)
			tc.mutate(query)
			ctx := newTestContext("?" + query.Encode())
			ctx.Request.AddCookie(&http.Cookie{Name: forumLoginFlowCookie, Value: flowID})
			uc := &UserCenter{Config: &Config{SSOHMACSecret: testSecret}, Logins: store, Guard: &fakeBindingGuard{}}
			if _, err := uc.ConnectorReceiver(ctx, ""); err == nil {
				t.Fatal("ambiguous/forged query was accepted")
			}
		})
	}
}

func TestConnectorReceiverRejectsMalformedBrowserFlow(t *testing.T) {
	store := newMemoryLoginStore()
	ticket := mintTicket("123", time.Now(), "nonce-malformed-flow")
	ctx := newTestContext("?" + ticketQuery(ticket).Encode())
	ctx.Request.AddCookie(&http.Cookie{Name: forumLoginFlowCookie, Value: "attacker-controlled"})
	uc := &UserCenter{Config: &Config{SSOHMACSecret: testSecret}, Logins: store, Guard: &fakeBindingGuard{}}
	if _, err := uc.ConnectorReceiver(ctx, ""); err == nil {
		t.Fatal("malformed browser flow cookie was accepted")
	}
}

func TestLegacyUserCenterCallbacksCannotBypassConnector(t *testing.T) {
	uc := &UserCenter{}
	if _, err := uc.LoginCallback(newTestContext("")); err == nil {
		t.Fatal("legacy user-center login callback stayed enabled")
	}
	if _, err := uc.SignUpCallback(newTestContext("")); err == nil {
		t.Fatal("legacy user-center sign-up callback stayed enabled")
	}
}

func TestConnectorDisplayNameFallsBackToUsername(t *testing.T) {
	store := newMemoryLoginStore()
	flowID := testFlowID("fallback")
	_ = store.Begin(context.Background(), flowID, time.Now().Add(loginFlowTTL))
	ticket := mintTicket("123", time.Now(), "nonce-fallback")
	ticket.DisplayName = ""
	mac := hmac.New(sha256.New, []byte(testSecret))
	_, _ = mac.Write([]byte(ticket.signingPayload()))
	ticket.Signature = hex.EncodeToString(mac.Sum(nil))
	ctx := newTestContext("?" + ticketQuery(ticket).Encode())
	ctx.Request.AddCookie(&http.Cookie{Name: forumLoginFlowCookie, Value: flowID})
	uc := &UserCenter{Config: &Config{SSOHMACSecret: testSecret}, Logins: store, Guard: &fakeBindingGuard{}}
	info, err := uc.ConnectorReceiver(ctx, "")
	if err != nil || info.DisplayName != "alice" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

func TestPulseOutageDegradesGracefully(t *testing.T) {
	uc := &UserCenter{Config: &Config{LevelBadgeEnabled: true}, Client: NewPulseClient(&Config{})}
	if branding := uc.PersonalBranding("123"); branding != nil {
		t.Errorf("branding = %v, want nil when Pulse is down", branding)
	}
}

func TestFormatContribution(t *testing.T) {
	for _, tc := range []struct {
		milli int64
		want  string
	}{{0, "0"}, {999, "0"}, {1000, "1"}, {1500, "1"}, {1234567, "1234"}} {
		if got := formatContribution(tc.milli); got != tc.want {
			t.Errorf("formatContribution(%d) = %q, want %q", tc.milli, got, tc.want)
		}
	}
}

func ticketQuery(ticket *LoginTicket) url.Values {
	return url.Values{
		"user_id": {ticket.UserID}, "username": {ticket.Username}, "display_name": {ticket.DisplayName},
		"email": {ticket.Email}, "avatar": {ticket.Avatar}, "timestamp": {strconv.FormatInt(ticket.Timestamp, 10)},
		"nonce": {ticket.Nonce}, "signature": {ticket.Signature},
	}
}

func testFlowID(label string) string {
	digest := sha256.Sum256([]byte(label))
	return hex.EncodeToString(digest[:])
}

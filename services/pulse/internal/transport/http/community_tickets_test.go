package transporthttp

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
	"testing"
)

type ticketGranter struct {
	calls int
	user  uint64
	admin []service.AdminTicketGrant
	actor string
	key   string
}

func (f *ticketGranter) AdminGrant(_ context.Context, actor, key string, g service.AdminTicketGrant) (service.AdminTicketGrantResult, error) {
	if g.Count < 1 || g.Count > service.MaxAdminTicketGrant {
		return service.AdminTicketGrantResult{}, service.ErrInvalidAdminTicketGrant
	}
	f.admin = append(f.admin, g)
	f.actor, f.key = actor, key
	return service.AdminTicketGrantResult{EntryID: 7, Count: g.Count}, nil
}

func (f *ticketGranter) Grant(_ context.Context, u uint64, _ service.CommunityTicket) (service.CommunityTicketResult, error) {
	f.calls++
	f.user = u
	return service.CommunityTicketResult{EntryID: 1}, nil
}
func TestCommunityTicketsSignedIdentityAndBody(t *testing.T) {
	for _, role := range []string{"community-bff", "admin", "new-api", "forum", ""} {
		f := &ticketGranter{}
		r := gin.New()
		CommunityTicketRoute(r.Group("/v1/internal"), f, func(c *gin.Context) { c.Set(PrincipalContextKey, security.Principal{UserID: 42, Role: role}) })
		w := settingsTestRequest(r, "POST", "/v1/internal/me/community-tickets", `{"day":"2026-11-06","category":"checkin","ordinal":1}`, "")
		if role != "community-bff" {
			if w.Code != 403 || f.calls != 0 {
				t.Fatal("unauthorized", role, w.Code)
			}
			continue
		}
		if w.Code != 200 || f.user != 42 {
			t.Fatal("identity", w.Code, f.user)
		}
		for _, body := range []string{`{"user_id":1}`, `{"amount":9}`, `{"day":"2026-11-06","day":"2026-11-07"}`, `{} {}`} {
			before := f.calls
			w = settingsTestRequest(r, "POST", "/v1/internal/me/community-tickets", body, "")
			if w.Code != 400 || f.calls != before {
				t.Fatal("untrusted payload", body, w.Code)
			}
		}
		w = settingsTestRequest(r, "POST", "/v1/internal/me/community-tickets?user_id=1", `{}`, "")
		if w.Code != 400 {
			t.Fatal("query identity")
		}
	}
}

func TestAdminTicketGrantRequiresAdminAndIdempotencyKey(t *testing.T) {
	for _, role := range []string{"admin", "community-bff", "forum", ""} {
		f := &ticketGranter{}
		r := gin.New()
		CommunityTicketRoute(r.Group("/v1/internal"), f, func(c *gin.Context) { c.Set(PrincipalContextKey, security.Principal{UserID: 9, Role: role}) })
		w := settingsTestRequest(r, "POST", "/v1/internal/admin/tickets/grant", `{"user_id":42,"count":10,"reason":"验证新概率"}`, "grant-1")
		if role != "admin" {
			if w.Code != 403 || len(f.admin) != 0 {
				t.Fatal("unauthorized", role, w.Code)
			}
			continue
		}
		if w.Code != 200 || len(f.admin) != 1 || f.admin[0].UserID != 42 || f.admin[0].Count != 10 || f.actor != "9" || f.key != "grant-1" {
			t.Fatalf("grant %d %+v %s %s", w.Code, f.admin, f.actor, f.key)
		}
		for _, c := range []struct{ body, key string }{
			{`{"user_id":42,"count":10,"reason":"x"}`, ""},
			{`{"user_id":42,"count":101,"reason":"验证"}`, "grant-2"},
			{`{"user_id":42,"count":1,"reason":"验证","extra":1}`, "grant-3"},
		} {
			if w = settingsTestRequest(r, "POST", "/v1/internal/admin/tickets/grant", c.body, c.key); w.Code != 400 {
				t.Fatal("invalid grant accepted", c.body, w.Code)
			}
		}
	}
}

func TestAdminTicketStatisticsBoundary(t *testing.T) {
	for _, role := range []string{"admin", "community-bff", "new-api", "forum"} {
		r := gin.New()
		CommunityTicketRoute(r.Group("/v1/internal"), &ticketGranter{}, func(c *gin.Context) { c.Set(PrincipalContextKey, security.Principal{UserID: 9, Role: role}) })
		for _, body := range []string{`{"user_ids":[]}`, `{"user_ids":[0]}`, `{"user_ids":[1],"extra":true}`, `{"user_ids":[1]} {}`} {
			w := settingsTestRequest(r, "POST", "/v1/internal/admin/tickets/statistics", body, "")
			want := 400
			if role != "admin" {
				want = 403
			}
			if w.Code != want {
				t.Fatalf("role=%s status=%d", role, w.Code)
			}
		}
	}
}

type statisticsTicketGranter struct{ ticketGranter }

func (*statisticsTicketGranter) Statistics(_ context.Context, ids []uint64) (map[uint64]ports.TicketStatistics, error) {
	result := map[uint64]ports.TicketStatistics{}
	for _, id := range ids {
		result[id] = ports.TicketStatistics{Remaining: 3, Total: 10}
	}
	return result, nil
}
func TestAdminTicketStatisticsSuccess(t *testing.T) {
	r := gin.New()
	CommunityTicketRoute(r.Group("/v1/internal"), &statisticsTicketGranter{}, func(c *gin.Context) { c.Set(PrincipalContextKey, security.Principal{UserID: 9, Role: "admin"}) })
	w := settingsTestRequest(r, "POST", "/v1/internal/admin/tickets/statistics", `{"user_ids":[42]}`, "")
	if w.Code != 200 || w.Body.String() != `{"42":{"remaining":"3","total":"10"}}` {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

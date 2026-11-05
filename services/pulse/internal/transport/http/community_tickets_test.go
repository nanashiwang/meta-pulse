package transporthttp

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
	"testing"
)

type ticketGranter struct {
	calls int
	user  uint64
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

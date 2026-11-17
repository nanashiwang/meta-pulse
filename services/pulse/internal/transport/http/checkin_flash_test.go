package transporthttp

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
	"testing"
)

type flashStub struct {
	calls int
	user  uint64
}

func (s *flashStub) GrantCheckinFlash(_ context.Context, u uint64, v service.CheckinFlash) (service.CheckinFlashResult, error) {
	s.calls++
	s.user = u
	return service.CheckinFlashResult{GrantID: "pg_test", Cents: v.Cents, Status: "pending"}, nil
}
func (s *flashStub) CheckinFlashStatus(_ context.Context, u uint64, id string) (service.CheckinFlashResult, error) {
	s.calls++
	s.user = u
	return service.CheckinFlashResult{GrantID: id, Cents: 30, Status: "settled"}, nil
}
func TestCheckinFlashRoutesRequireSignedCommunityAndRejectClientFields(t *testing.T) {
	for _, role := range []string{"community-bff", "admin", "forum", "new-api", ""} {
		s := &flashStub{}
		r := gin.New()
		CheckinFlashRoutes(r.Group("/v1/internal"), s, func(c *gin.Context) { c.Set(PrincipalContextKey, security.Principal{UserID: 42, Role: role}) })
		w := settingsTestRequest(r, "POST", "/v1/internal/me/checkin-flash", `{"day":"2026-11-18","cents":30,"event_id":1}`, "")
		if role != "community-bff" {
			if w.Code != 403 || s.calls != 0 {
				t.Fatal(role, w.Code)
			}
			continue
		}
		if w.Code != 200 || s.user != 42 {
			t.Fatal(w.Code, s.user)
		}
		for _, body := range []string{`{"day":"2026-11-18","user_id":9,"cents":30,"event_id":1}`, `{"cents":1,"cents":150}`, `{"amount":999}`} {
			n := s.calls
			w = settingsTestRequest(r, "POST", "/v1/internal/me/checkin-flash", body, "")
			if w.Code != 400 || s.calls != n {
				t.Fatal(body, w.Code)
			}
		}
		w = settingsTestRequest(r, "GET", "/v1/internal/me/checkin-flash/pg_test?user_id=9", "", "")
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
		w = settingsTestRequest(r, "GET", "/v1/internal/me/checkin-flash/pg_test", "", "")
		if w.Code != 200 || s.user != 42 {
			t.Fatal(w.Code)
		}
	}
}

package transporthttp

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
	"testing"
)

type experienceFixture struct {
	calls int
	user  uint64
	actor string
}

func (f *experienceFixture) Pending(_ context.Context, user uint64) ([]service.ExperienceDelivery, error) {
	f.calls++
	f.user = user
	return []service.ExperienceDelivery{}, nil
}
func (f *experienceFixture) Acknowledge(_ context.Context, user uint64, _, _ string) error {
	f.calls++
	f.user = user
	return nil
}
func (f *experienceFixture) Reverse(_ context.Context, actor, _, _, _ string) error {
	f.calls++
	f.actor = actor
	return nil
}
func TestExperienceRoutesIdentityAndRoles(t *testing.T) {
	for _, role := range []string{"admin", "community-bff", "forum", "new-api", ""} {
		f := &experienceFixture{}
		r := gin.New()
		ExperienceRoutes(r.Group("/v1/internal"), f, func(c *gin.Context) { c.Set(PrincipalContextKey, security.Principal{UserID: 42, Role: role}); c.Next() })
		for _, tc := range []struct{ method, path, body, role string }{
			{"GET", "/me/experience", "", "community-bff"},
			{"POST", "/me/experience/ack", `{"grant_id":"g1","status":"pending"}`, "community-bff"},
			{"POST", "/admin/experience/reverse", `{"grant_id":"g1","reason":"test"}`, "admin"},
		} {
			before := f.calls
			w := settingsTestRequest(r, tc.method, "/v1/internal"+tc.path, tc.body, "request-1")
			if role == tc.role {
				if w.Code != 200 || f.calls != before+1 {
					t.Fatalf("authorized %s %s: %d", role, tc.path, w.Code)
				}
			} else if w.Code != 403 || f.calls != before {
				t.Fatalf("unauthorized %s %s", role, tc.path)
			}
		}
		if role == "community-bff" {
			if f.user != 42 {
				t.Fatal("identity not derived from principal")
			}
			for _, b := range []string{`{"user_id":99}`, `{"amount":999}`, `{"grant_id":"a","grant_id":"b"}`, `{"status":null}`, `{} {}`} {
				before := f.calls
				w := settingsTestRequest(r, "POST", "/v1/internal/me/experience/ack", b, "request-2")
				if w.Code != 400 || f.calls != before {
					t.Fatal("invalid acknowledgement accepted", b)
				}
			}
			if w := settingsTestRequest(r, "GET", "/v1/internal/me/experience?user_id=99", "", "request-3"); w.Code != 400 {
				t.Fatal("query identity accepted")
			}
		}
		if role == "admin" && f.actor != "42" {
			t.Fatal("actor not derived from principal")
		}
	}
}

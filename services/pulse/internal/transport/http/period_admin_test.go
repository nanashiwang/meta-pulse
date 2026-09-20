package transporthttp

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type periodAdminFixture struct {
	calls      int
	actor, key string
}

func (f *periodAdminFixture) ListForAdmin(context.Context) ([]service.AdminPeriodView, error) {
	f.calls++
	return []service.AdminPeriodView{}, nil
}
func (f *periodAdminFixture) CreateFromAdmin(_ context.Context, _ service.PeriodAdminRequest, actor, key string) (service.PeriodCreateResult, error) {
	f.calls++
	f.actor, f.key = actor, key
	return service.PeriodCreateResult{}, nil
}
func TestPeriodAdminRoutes(t *testing.T) {
	for _, role := range []string{"admin", "community-bff", "forum", "new-api", ""} {
		f := &periodAdminFixture{}
		r := gin.New()
		PeriodAdminRoutes(r.Group("/v1/internal"), f, func(c *gin.Context) { c.Set(PrincipalContextKey, security.Principal{UserID: 42, Role: role}); c.Next() })
		w := settingsTestRequest(r, "PUT", "/v1/internal/admin/periods", `{"key":"new"}`, "request-1")
		if role == "admin" {
			if w.Code != 200 || f.actor != "42" || f.key != "request-1" {
				t.Fatalf("admin rejected: %s", w.Body)
			}
		} else if w.Code != 403 || f.calls != 0 {
			t.Fatalf("unauthorized role %s", role)
		}
		if role != "admin" {
			continue
		}
		for _, body := range []string{`{"actor_id":4}`, `{"key":"a","key":"b"}`, `{"key":null}`, `{"rewards":[{"key":"a","key":"b"}]}`, `{} {}`} {
			before := f.calls
			w = settingsTestRequest(r, "PUT", "/v1/internal/admin/periods", body, "request-2")
			if w.Code != 400 || f.calls != before {
				t.Fatalf("invalid body accepted %s", body)
			}
		}
		if w := settingsTestRequest(r, "PUT", "/v1/internal/admin/periods", `{}`, ""); w.Code != 400 {
			t.Fatal("missing key accepted")
		}
	}
}

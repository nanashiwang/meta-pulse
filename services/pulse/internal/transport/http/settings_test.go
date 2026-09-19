package transporthttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/runtimeconfig"
	"github.com/nanashiwang/meta-pulse/internal/security"
)

type settingsFixture struct {
	calls      int
	actor, key string
	err        error
}

func (f *settingsFixture) View(context.Context) (runtimeconfig.View, error) {
	f.calls++
	return runtimeconfig.View{}, f.err
}
func (f *settingsFixture) Update(_ context.Context, _ runtimeconfig.UpdateRequest, actor, key string) (runtimeconfig.View, error) {
	f.calls++
	f.actor, f.key = actor, key
	return runtimeconfig.View{}, f.err
}
func settingsTestRouter(f *settingsFixture, role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RuntimeSettingsRoutes(r.Group("/v1/internal"), f, func(c *gin.Context) { c.Set(PrincipalContextKey, security.Principal{UserID: 42, Role: role}); c.Next() })
	return r
}
func settingsTestRequest(r http.Handler, method, path, body, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestRuntimeSettingsRequiresAdministratorAndDerivesActor(t *testing.T) {
	for _, role := range []string{"", "forum", "community-bff", "new-api", "worker"} {
		f := &settingsFixture{}
		w := settingsTestRequest(settingsTestRouter(f, role), "PUT", "/v1/internal/admin/settings", `{"revision":0,"reason":"test"}`, "request-1")
		if w.Code != 403 || f.calls != 0 {
			t.Fatalf("role %q reached settings", role)
		}
	}
	f := &settingsFixture{}
	w := settingsTestRequest(settingsTestRouter(f, "admin"), "PUT", "/v1/internal/admin/settings", `{"revision":0,"reason":"test","config":{"actions_enabled":false}}`, "request-1")
	if w.Code != 200 || f.actor != "42" || f.key != "request-1" {
		t.Fatal("trusted actor/idempotency was not forwarded")
	}
}
func TestRuntimeSettingsRejectsAmbiguousOrUnboundedJSON(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `{} {}`, `{"actor_id":"1"}`, `{"revision":0,"revision":1}`, `{"config":{"quota_per_unit":"1","quota_per_unit":"2"}}`,
		`{"secrets":{"PULSE_ADMIN_HMAC_SECRET":"a","PULSE_ADMIN_HMAC_SECRET":"b"}}`, `{"config":{"unknown":true}}`, `{"config":null}`, strings.Repeat(" ", 32769),
	} {
		f := &settingsFixture{}
		w := settingsTestRequest(settingsTestRouter(f, "admin"), "PUT", "/v1/internal/admin/settings", body, "request-1")
		if w.Code != 400 || f.calls != 0 {
			t.Fatalf("accepted malformed JSON: %q", body[:min(len(body), 100)])
		}
	}
	f := &settingsFixture{}
	r := settingsTestRouter(f, "admin")
	for _, key := range []string{"", "space key", strings.Repeat("x", 97)} {
		if w := settingsTestRequest(r, "PUT", "/v1/internal/admin/settings", `{}`, key); w.Code != 400 {
			t.Fatal("accepted invalid idempotency key")
		}
	}
	if w := settingsTestRequest(r, "GET", "/v1/internal/admin/settings?secret=hidden", "", ""); w.Code != 400 {
		t.Fatal("accepted query")
	}
}
func TestRuntimeSettingsNeverReturnsUnderlyingErrors(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{{errors.Join(runtimeconfig.ErrInvalid, errors.New("do-not-leak-secret")), 400}, {runtimeconfig.ErrConflict, 409}, {errors.New("do-not-leak-secret"), 503}} {
		f := &settingsFixture{err: test.err}
		w := settingsTestRequest(settingsTestRouter(f, "admin"), "GET", "/v1/internal/admin/settings", "", "")
		if w.Code != test.status || strings.Contains(w.Body.String(), "do-not-leak") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("unsafe error response")
		}
	}
}
func TestRuntimeSecretGenerationRequiresAdminAndDoesNotPersist(t *testing.T) {
	f := &settingsFixture{}
	r := settingsTestRouter(f, "admin")
	a := settingsTestRequest(r, "POST", "/v1/internal/admin/settings/secret", `{}`, "")
	b := settingsTestRequest(r, "POST", "/v1/internal/admin/settings/secret", `{}`, "")
	if a.Code != 200 || b.Code != 200 || a.Body.String() == b.Body.String() || f.calls != 0 {
		t.Fatal("secret generation failed or persisted")
	}
	if w := settingsTestRequest(r, "POST", "/v1/internal/admin/settings/secret", `{"field":"x"}`, ""); w.Code != 400 {
		t.Fatal("accepted secret generation fields")
	}
}

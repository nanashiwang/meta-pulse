package pulse_user_center

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/marketplace"
)

func TestMarketplaceRequestBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	uc := &UserCenter{}
	for _, tc := range []struct {
		body, token, origin, query string
		admin                      bool
		status                     int
	}{
		{`{"op":"submit"}`, "", "https://metarhub.com", "", false, 401},
		{`{"op":"submit"}`, "test", "https://evil.invalid", "", false, 403},
		{`{"op":"submit"}`, "test", "https://metarhub.com", "?user_id=3", false, 400},
		{`{"op":"review"}`, "test", "https://metarhub.com", "", false, 403},
		{`{"op":"submit"}`, "test", "https://metarhub.com", "", true, 403},
		{`{"op":"submit","user_id":"3"}`, "test", "https://metarhub.com", "", false, 400},
		{`{"op":"submit","op":"archive"}`, "test", "https://metarhub.com", "", false, 400},
		{`{"op":"submit","entry":{"kind":"project","kind":"idea"}}`, "test", "https://metarhub.com", "", false, 400},
		{`{"op":"submit"} {}`, "test", "https://metarhub.com", "", false, 400},
		{`{"op":"submit","entry":{"demo_url":"https://metarhub.com/test"}}`, "test", "https://metarhub.com", "", false, 400},
	} {
		r := gin.New()
		r.POST("/command", uc.marketplaceHandler("command", func(*gin.Context) (string, error) { return "2", nil }, tc.admin, func() string { return "https://metarhub.com" }))
		req := httptest.NewRequest("POST", "/command"+tc.query, strings.NewReader(tc.body))
		req.Header.Set("Authorization", tc.token)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Metar-Request", "1")
		req.Header.Set("Idempotency-Key", "stable-request")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%s got %d want %d", tc.body, w.Code, tc.status)
		}
	}
}
func TestMarketplaceRoutesAreDistinct(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	uc := &UserCenter{}
	session := func(*gin.Context) (string, error) { return "2", nil }
	site := func() string { return "https://metarhub.com" }
	uc.registerMarketplacePublic(r.Group("/answer/api/v1"))
	uc.registerMarketplaceUser(r.Group("/answer/api/v1"), session, site)
	uc.registerMarketplaceAdmin(r.Group("/answer/admin/api"), session, site)
	if len(r.Routes()) != 6 {
		t.Fatal(r.Routes())
	}
	for _, path := range []string{"/answer/api/v1/metar/marketplace/items?user_id=2", "/answer/api/v1/metar/marketplace/items?page=01", "/answer/api/v1/metar/marketplace/items?kind=project&kind=idea"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatal(path, w.Code)
		}
	}
	var c marketplace.Command
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"op":"submit"} {}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if marketplaceJSON(ctx, &c) {
		t.Fatal("trailing data accepted")
	}
}

package http

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/service"
)

type fakeReader struct {
	err       error
	lastQuery service.Query
	lastSlug  string
}

func (f *fakeReader) Home(context.Context) (service.Home, error) { return service.Home{}, f.err }
func (f *fakeReader) Leaderboard(_ context.Context, q service.Query) (service.Board, error) {
	f.lastQuery = q
	return service.Board{}, f.err
}
func (f *fakeReader) Model(_ context.Context, slug string) (service.ModelDetail, error) {
	f.lastSlug = slug
	return service.ModelDetail{}, f.err
}
func (f *fakeReader) Methodology(context.Context, time.Duration) (service.Methodology, error) {
	return service.Methodology{}, f.err
}

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func do(t *testing.T, r *fakeReader, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	NewRouter(nil, r, fakeDB{}, time.Hour).ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestLeaderboardQueryParsing(t *testing.T) {
	r := &fakeReader{}
	if w := do(t, r, "GET", "/v1/public/leaderboard?window=30d&sort=price&order=asc&open=1&page=2"); w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	q := r.lastQuery
	if q.Window != "30d" || q.Sort != "price" || !q.Asc || !q.OpenOnly || q.Page != 2 {
		t.Fatalf("query %+v", q)
	}
	for _, bad := range []string{"?sort=name", "?window=7d", "?page=0", "?page=x", "?order=up", "?open=yes", "?utm=1", "?page=1&page=2"} {
		if w := do(t, r, "GET", "/v1/public/leaderboard"+bad); w.Code != 400 {
			t.Errorf("%s: status %d", bad, w.Code)
		}
	}
}

func TestErrorMapping(t *testing.T) {
	cases := map[error]int{
		nil:                   200,
		service.ErrNoSnapshot: 404,
		service.ErrNotFound:   404,
		errors.New("db down"): 503,
	}
	for err, want := range cases {
		w := do(t, &fakeReader{err: err}, "GET", "/v1/public/home")
		if w.Code != want {
			t.Errorf("%v: status %d, want %d", err, w.Code, want)
		}
		if want == 200 && w.Header().Get("Cache-Control") != publicCache {
			t.Error("success must be publicly cacheable")
		}
		if want == 503 && w.Header().Get("Cache-Control") != "no-store" {
			t.Error("outage must not be cached")
		}
	}
}

func TestModelSlugValidationAndReadOnly(t *testing.T) {
	r := &fakeReader{}
	if w := do(t, r, "GET", "/v1/public/models/claude-x.5"); w.Code != 200 || r.lastSlug != "claude-x.5" {
		t.Fatalf("valid slug: %d %q", w.Code, r.lastSlug)
	}
	r.lastSlug = ""
	if w := do(t, r, "GET", "/v1/public/models/Bad_Slug"); w.Code != 404 || r.lastSlug != "" {
		t.Fatal("invalid slug must not reach the store")
	}
	if w := do(t, r, "POST", "/v1/public/home"); w.Code != 405 {
		t.Fatalf("POST must be rejected, got %d", w.Code)
	}
	if w := do(t, r, "GET", "/v1/internal/anything"); w.Code != 404 {
		t.Fatal("no internal routes exist")
	}
}

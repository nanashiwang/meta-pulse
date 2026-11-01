package pulse_user_center

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/imageimport"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeImageDownloader struct{ calls int }

func (f *fakeImageDownloader) Fetch(context.Context, string) (imageimport.Image, error) {
	f.calls++
	return imageimport.Image{Bytes: []byte("PNG"), MIME: "image/png"}, nil
}
func TestImageImportPermissions(t *testing.T) {
	for _, tt := range []struct {
		name, auth, origin, body, query string
		accountErr                      error
		status                          int
	}{
		{name: "ok", auth: "session", origin: "https://community.test", body: `{"url":"https://example.org/a.png"}`, status: 200},
		{name: "anonymous", origin: "https://community.test", body: `{"url":"https://example.org/a.png"}`, status: 401},
		{name: "cross origin", auth: "session", origin: "https://evil.test", body: `{"url":"https://example.org/a.png"}`, status: 403},
		{name: "inactive", auth: "session", origin: "https://community.test", body: `{"url":"https://example.org/a.png"}`, accountErr: errCommunityAccountUnavailable, status: 403},
		{name: "database fails", auth: "session", origin: "https://community.test", body: `{"url":"https://example.org/a.png"}`, accountErr: errors.New("offline"), status: 503},
		{name: "extra identity", auth: "session", origin: "https://community.test", body: `{"url":"https://example.org/a.png","user_id":"2"}`, status: 400},
		{name: "duplicate", auth: "session", origin: "https://community.test", body: `{"url":"a","url":"b"}`, status: 400},
		{name: "query token", auth: "session", origin: "https://community.test", query: "?token=secret", body: `{"url":"https://example.org/a.png"}`, status: 401},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := &fakeImageDownloader{}
			r := gin.New()
			r.POST("/fetch", imageImportHandler(func(*gin.Context) (string, error) { return "1", nil }, func(context.Context, string) error { return tt.accountErr }, func() string { return "https://community.test" }, d, &imageLimiter{}))
			req := httptest.NewRequest("POST", "/fetch"+tt.query, strings.NewReader(tt.body))
			req.Header.Set("Authorization", tt.auth)
			req.Header.Set("Origin", tt.origin)
			req.Header.Set("X-Metar-Request", "1")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if w.Code != 200 && d.calls > 0 {
				t.Fatal("unauthorized download")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable")
			}
		})
	}
}
func TestImageImportLimiter(t *testing.T) {
	l := &imageLimiter{}
	now := time.Now()
	release, ok := l.acquire("1", now)
	if !ok {
		t.Fatal("rejected")
	}
	if _, ok = l.acquire("1", now); ok {
		t.Fatal("concurrent user")
	}
	release()
	for i := 1; i < 60; i++ {
		done, allowed := l.acquire("1", now)
		if !allowed {
			t.Fatal(i)
		}
		done()
	}
	if _, ok = l.acquire("1", now); ok {
		t.Fatal("rate limit bypass")
	}
	done, ok := l.acquire("1", now.Add(time.Minute))
	if !ok {
		t.Fatal("window not reset")
	}
	done()
	var releases []func()
	for _, u := range []string{"a", "b", "c", "d"} {
		done, ok := l.acquire(u, now)
		if !ok {
			t.Fatal(u)
		}
		releases = append(releases, done)
	}
	if _, ok = l.acquire("e", now); ok {
		t.Fatal("global concurrency bypass")
	}
	for _, done := range releases {
		done()
	}
}

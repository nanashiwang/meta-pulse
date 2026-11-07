package auth

import (
	"context"
	"encoding/json"
	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/entity"
	"github.com/segmentfault/pacman/cache"
	"sync"
	"testing"
	"time"
)

type item struct {
	value   string
	expires time.Time
}
type clockCache struct {
	cache.Cache
	mu    sync.Mutex
	now   time.Time
	items map[string]item
}

func (c *clockCache) GetString(_ context.Context, key string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.items[key]
	return v.value, ok && v.expires.After(c.now), nil
}
func (c *clockCache) SetString(_ context.Context, key, value string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = item{value, c.now.Add(ttl)}
	return nil
}
func (c *clockCache) Del(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
	return nil
}
func fixture(t *testing.T) (*authRepo, *clockCache, context.Context) {
	t.Helper()
	c := &clockCache{now: time.Unix(100000, 0), items: map[string]item{}}
	r := &authRepo{data: &data.Data{Cache: c}}
	ctx := context.Background()
	if err := r.SetUserCacheInfo(ctx, "token", "visit", &entity.UserCacheInfo{UserID: "7", UserStatus: 1, EmailStatus: 1}); err != nil {
		t.Fatal(err)
	}
	return r, c, ctx
}
func TestMetarSlidingRetention(t *testing.T) {
	r, c, ctx := fixture(t)
	for i := 0; i < 3; i++ {
		c.now = c.now.Add(29 * 24 * time.Hour)
		info, err := r.GetUserCacheInfo(ctx, "token")
		if err != nil || info == nil {
			t.Fatalf("renew %v %v", info, err)
		}
		for _, key := range []string{constant.UserTokenCacheKey + "token", constant.UserVisitTokenCacheKey + "visit", constant.UserTokenMappingCacheKey + "7"} {
			if got := c.items[key].expires.Sub(c.now); got != 30*24*time.Hour {
				t.Fatalf("%s ttl=%v", key, got)
			}
		}
	}
	c.now = c.now.Add(30 * 24 * time.Hour)
	if info, err := r.GetUserCacheInfo(ctx, "token"); err != nil || info != nil {
		t.Fatal("expired token revived")
	}
}
func TestMetarRevocationAndStatus(t *testing.T) {
	t.Run("logout", func(t *testing.T) {
		r, _, ctx := fixture(t)
		if err := r.RemoveUserCacheInfo(ctx, "token"); err != nil {
			t.Fatal(err)
		}
		if u, _ := r.GetUserCacheInfo(ctx, "token"); u != nil {
			t.Fatal("revived")
		}
		if v, _ := r.GetUserVisitCacheInfo(ctx, "visit"); v != "" {
			t.Fatal("visit survived")
		}
	})
	t.Run("password reset after 7 days", func(t *testing.T) {
		r, c, ctx := fixture(t)
		c.now = c.now.Add(29 * 24 * time.Hour)
		r.GetUserCacheInfo(ctx, "token")
		r.RemoveUserTokens(ctx, "7", "")
		if u, _ := r.GetUserCacheInfo(ctx, "token"); u != nil {
			t.Fatal("index failed to revoke")
		}
	})
	t.Run("suspended", func(t *testing.T) {
		r, c, ctx := fixture(t)
		before := c.items[constant.UserTokenCacheKey+"token"].expires
		c.now = c.now.Add(time.Hour)
		r.SetUserStatus(ctx, "7", &entity.UserCacheInfo{UserID: "7", UserStatus: 9})
		u, e := r.GetUserCacheInfo(ctx, "token")
		if e != nil || u.UserStatus != 9 {
			t.Fatal("status ignored")
		}
		if !c.items[constant.UserTokenCacheKey+"token"].expires.Equal(before) {
			t.Fatal("suspended renewed")
		}
	})
	t.Run("concurrent logout", func(t *testing.T) {
		r, _, ctx := fixture(t)
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); r.GetUserCacheInfo(ctx, "token") }()
		}
		r.RemoveUserCacheInfo(ctx, "token")
		wg.Wait()
		if u, _ := r.GetUserCacheInfo(ctx, "token"); u != nil {
			t.Fatal("concurrent resurrection")
		}
	})
	t.Run("corrupt", func(t *testing.T) {
		r, c, ctx := fixture(t)
		c.SetString(ctx, constant.UserTokenCacheKey+"token", "{", time.Hour)
		if u, e := r.GetUserCacheInfo(ctx, "token"); e == nil || u != nil {
			t.Fatal("corrupt renewed")
		}
	})
	t.Run("retain index", func(t *testing.T) {
		r, c, ctx := fixture(t)
		r.RemoveUserTokens(ctx, "7", "token")
		var m map[string]bool
		json.Unmarshal([]byte(c.items[constant.UserTokenMappingCacheKey+"7"].value), &m)
		if !m["token"] {
			t.Fatal("lost retained session")
		}
	})
}

package pulse_user_center

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/imageimport"
)

type imageAccountReader interface {
	CheckImageAccount(context.Context, string) error
}

func (g *mysqlBindingGuard) CheckImageAccount(ctx context.Context, user string) error {
	var status, mail int
	if err := g.db.QueryRowContext(ctx, "SELECT status, mail_status FROM `user` WHERE id = ?", user).Scan(&status, &mail); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errCommunityAccountUnavailable
		}
		return err
	}
	if status != 1 || mail != 1 {
		return errCommunityAccountUnavailable
	}
	return nil
}

type imageQuota struct {
	since time.Time
	count int
	busy  bool
}
type imageLimiter struct {
	mu     sync.Mutex
	users  map[string]*imageQuota
	active int
}

func (l *imageLimiter) acquire(user string, now time.Time) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.users == nil {
		l.users = make(map[string]*imageQuota)
	}
	for id, q := range l.users {
		if !q.busy && now.Sub(q.since) >= time.Minute {
			delete(l.users, id)
		}
	}
	q := l.users[user]
	if q == nil {
		if len(l.users) >= 4096 {
			return nil, false
		}
		q = &imageQuota{since: now}
		l.users[user] = q
	}
	if q.busy || q.count >= 60 || l.active >= 4 {
		return nil, false
	}
	q.count++
	q.busy = true
	l.active++
	return func() { l.mu.Lock(); defer l.mu.Unlock(); q.busy = false; l.active-- }, true
}

type imageDownloader interface {
	Fetch(context.Context, string) (imageimport.Image, error)
}

func imageImportHandler(session func(*gin.Context) (string, error), check func(context.Context, string) error, site func() string, downloader imageDownloader, limit *imageLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Robots-Tag", "noindex, nofollow")
		if c.GetHeader("Authorization") == "" || c.Request.URL.RawQuery != "" {
			communityError(c, 401, "authentication_required")
			return
		}
		user, err := session(c)
		if err != nil {
			communityIdentityError(c, err)
			return
		}
		if !communitySameOrigin(c.Request, site()) {
			communityError(c, 403, "origin_rejected")
			return
		}
		var req struct {
			URL string `json:"url"`
		}
		if !chatJSON(c, &req) || len(req.URL) > 4096 || req.URL == "" {
			communityError(c, 400, "invalid_request")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 18*time.Second)
		defer cancel()
		if err = check(ctx, user); err != nil {
			communityIdentityError(c, err)
			return
		}
		release, ok := limit.acquire(user, time.Now())
		if !ok {
			c.Header("Retry-After", "60")
			communityError(c, 429, "rate_limited")
			return
		}
		defer release()
		result, err := downloader.Fetch(ctx, req.URL)
		if err != nil {
			communityError(c, 422, "image_unavailable")
			return
		}
		c.Data(http.StatusOK, result.MIME, result.Bytes)
	}
}
func (uc *UserCenter) registerImageImport(r *gin.RouterGroup, session func(*gin.Context) (string, error), site func() string) {
	check := func(ctx context.Context, user string) error {
		uc.runtimeMu.RLock()
		guard := uc.Guard
		uc.runtimeMu.RUnlock()
		reader, ok := guard.(imageAccountReader)
		if !ok {
			return errors.New("image import unavailable")
		}
		return reader.CheckImageAccount(ctx, user)
	}
	r.POST("/metar/images/fetch", imageImportHandler(session, check, site, imageimport.New(), &imageLimiter{}))
}

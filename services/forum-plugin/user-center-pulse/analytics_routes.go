package pulse_user_center

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/apache/answer/plugin"
	"github.com/gin-gonic/gin"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/analytics"
)

var opsLimiter imageLimiter

func (uc *UserCenter) operationsStore(ctx context.Context) (*analytics.Store, error) {
	uc.opsMu.Lock()
	defer uc.opsMu.Unlock()
	if uc.opsStore != nil {
		return uc.opsStore, nil
	}
	cfg, e := mysql.ParseDSN(os.Getenv("FORUM_BINDING_GUARD_DSN"))
	if e != nil || cfg.DBName == "" {
		return nil, errors.New("analytics database unavailable")
	}
	if cfg.TLSConfig == "skip-verify" || cfg.TLSConfig == "preferred" {
		return nil, errors.New("unverified mysql TLS")
	}
	cfg.MultiStatements = false
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Timeout = 3 * time.Second
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 3 * time.Second
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	db, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	s := analytics.New(db)
	if e = s.Ensure(ctx); e != nil {
		db.Close()
		return nil, e
	}
	uc.opsStore = s
	return s, nil
}
func (uc *UserCenter) registerOperations(r *gin.RouterGroup, session func(*gin.Context) (string, error), admin bool) {
	if admin {
		r.GET("/metar/analytics/report", uc.operationsAdmin("report", session, plugin.SiteURL))
		r.GET("/metar/analytics/members", uc.operationsAdmin("members", session, plugin.SiteURL))
		r.PUT("/metar/analytics/labels", uc.operationsAdmin("labels", session, plugin.SiteURL))
		return
	}
	path := "/metar/analytics/visit"
	if session == nil {
		path = "/metar/analytics/anonymous"
	}
	r.POST(path, uc.operationsVisit(session, plugin.SiteURL))
}
func operationsHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Robots-Tag", "noindex, nofollow")
}
func operationsError(c *gin.Context, e error) {
	status, code := 503, "analytics_unavailable"
	switch {
	case errors.Is(e, analytics.ErrForbidden):
		status, code = 403, "forbidden"
	case errors.Is(e, analytics.ErrInvalid):
		status, code = 400, "invalid_request"
	case errors.Is(e, analytics.ErrConflict):
		status, code = 409, "conflict"
	case errors.Is(e, analytics.ErrLimit):
		status, code = 422, "analytics_limit"
	}
	communityError(c, status, code)
}
func (uc *UserCenter) operationsVisit(session func(*gin.Context) (string, error), site func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		operationsHeaders(c)
		if c.Request.URL.RawQuery != "" || c.GetHeader("X-Metar-Request") != "1" || !communitySameOrigin(c.Request, site()) {
			communityError(c, 403, "origin_rejected")
			return
		}
		var v analytics.Visit
		if !experienceJSON(c, &v) || v.Validate() != nil {
			communityError(c, 400, "invalid_request")
			return
		}
		user := ""
		var e error
		if session != nil {
			if c.GetHeader("Authorization") == "" {
				communityError(c, 401, "authentication_required")
				return
			}
			user, e = session(c)
			if e != nil {
				communityIdentityError(c, e)
				return
			}
		} else if c.GetHeader("Authorization") != "" {
			communityError(c, 400, "invalid_request")
			return
		}
		// Known crawlers are not measured. This is a noise filter, not proof of humanity.
		ua := strings.ToLower(c.Request.UserAgent())
		for _, bot := range []string{"bot", "spider", "crawler", "headless", "preview"} {
			if strings.Contains(ua, bot) {
				c.Status(204)
				return
			}
		}
		release, ok := opsLimiter.acquire(c.ClientIP(), time.Now())
		if !ok {
			communityError(c, 429, "rate_limited")
			return
		}
		defer release()
		cookie, _ := c.Cookie("metar_ops")
		fresh := !analytics.CookiePattern.MatchString(cookie)
		if fresh {
			cookie, e = analytics.NewVisitor()
			if e != nil {
				communityError(c, 503, "analytics_unavailable")
				return
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		store, e := uc.operationsStore(ctx)
		if e != nil {
			operationsError(c, e)
			return
		}
		if e = store.Track(ctx, cookie, user, v); e != nil {
			operationsError(c, e)
			return
		}
		if fresh {
			http.SetCookie(c.Writer, &http.Cookie{Name: "metar_ops", Value: cookie, Path: "/answer/api/v1/metar/analytics", MaxAge: 90 * 86400, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		}
		c.Status(204)
	}
}
func (uc *UserCenter) operationsAdmin(op string, session func(*gin.Context) (string, error), site func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		operationsHeaders(c)
		if c.GetHeader("Authorization") == "" {
			communityError(c, 401, "authentication_required")
			return
		}
		user, e := session(c)
		if e != nil {
			adminSettingsIdentityError(c, e)
			return
		}
		q, e := url.ParseQuery(c.Request.URL.RawQuery)
		if e != nil {
			communityError(c, 400, "invalid_request")
			return
		}
		for k, v := range q {
			if len(v) != 1 || !(op == "report" && k == "days" || op == "members" && (k == "page" || k == "q" || k == "label")) {
				communityError(c, 400, "invalid_request")
				return
			}
		}
		days, page := 30, 1
		if q.Has("days") {
			days, e = strconv.Atoi(q.Get("days"))
			if e != nil || (days != 7 && days != 30 && days != 90) {
				communityError(c, 400, "invalid_request")
				return
			}
		}
		if q.Has("page") {
			page, e = strconv.Atoi(q.Get("page"))
			if e != nil || page < 1 || page > 1000 {
				communityError(c, 400, "invalid_request")
				return
			}
		}
		var cmd analytics.LabelCommand
		if op == "labels" {
			if !communitySameOrigin(c.Request, site()) {
				communityError(c, 403, "origin_rejected")
				return
			}
			if !experienceJSON(c, &cmd) || cmd.Validate() != nil || len(c.Request.Header.Values("Idempotency-Key")) != 1 || !analytics.KeyPattern.MatchString(c.GetHeader("Idempotency-Key")) {
				communityError(c, 400, "invalid_request")
				return
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		store, e := uc.operationsStore(ctx)
		if e != nil {
			operationsError(c, e)
			return
		}
		var result any
		switch op {
		case "report":
			result, e = store.Report(ctx, user, days)
		case "members":
			result, e = store.Members(ctx, user, q.Get("q"), q.Get("label"), page)
		case "labels":
			var version int64
			version, e = store.SaveLabels(ctx, user, c.GetHeader("Idempotency-Key"), cmd)
			result = gin.H{"version": version}
		}
		if e != nil {
			operationsError(c, e)
			return
		}
		c.JSON(200, gin.H{"code": 200, "data": result})
	}
}

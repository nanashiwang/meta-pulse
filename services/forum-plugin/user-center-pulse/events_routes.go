package pulse_user_center

import (
	"context"
	"database/sql"
	"errors"
	"github.com/gin-gonic/gin"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/events"
	"net/url"
	"os"
	"strconv"
	"time"
)

func (uc *UserCenter) communityEventStore(ctx context.Context) (*events.Store, error) {
	uc.eventMu.Lock()
	defer uc.eventMu.Unlock()
	if uc.eventStore != nil {
		return uc.eventStore, nil
	}
	cfg, err := mysql.ParseDSN(os.Getenv("FORUM_BINDING_GUARD_DSN"))
	if err != nil || cfg.DBName == "" {
		return nil, errors.New("events database unavailable")
	}
	if cfg.TLSConfig == "skip-verify" || cfg.TLSConfig == "preferred" {
		return nil, errors.New("Forum MySQL TLS must verify the server")
	}
	cfg.MultiStatements = false
	cfg.ParseTime = true
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 15 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 15 * time.Second
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	store := events.New(db)
	if err = store.Ensure(ctx); err != nil {
		db.Close()
		return nil, err
	}
	uc.eventStore = store
	return store, nil
}

func (uc *UserCenter) registerEvents(r *gin.RouterGroup, session func(*gin.Context) (string, error), admin bool, site func() string) {
	r.GET("/metar/events/mine", uc.eventHandler("list", session, admin, site))
	r.POST("/metar/events/command", uc.eventHandler("command", session, admin, site))
	if admin {
		r.GET("/metar/events/roster", uc.eventHandler("roster", session, true, site))
	}
}
func (uc *UserCenter) eventHandler(op string, session func(*gin.Context) (string, error), admin bool, site func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Robots-Tag", "noindex, nofollow")
		user := ""
		var err error
		if session != nil {
			if c.GetHeader("Authorization") == "" {
				communityError(c, 401, "authentication_required")
				return
			}
			user, err = session(c)
			if err != nil {
				communityIdentityError(c, err)
				return
			}
		}
		if c.Request.Method != "GET" && !communitySameOrigin(c.Request, site()) {
			communityError(c, 403, "origin_rejected")
			return
		}
		q, err := url.ParseQuery(c.Request.URL.RawQuery)
		if err != nil {
			communityError(c, 400, "invalid_request")
			return
		}
		for k, v := range q {
			if len(v) != 1 || !(op == "list" && (k == "page" || k == "past") || op == "roster" && (k == "page" || k == "id")) {
				communityError(c, 400, "invalid_request")
				return
			}
		}
		page := 1
		if q.Has("page") {
			page, err = strconv.Atoi(q.Get("page"))
			if err != nil || page < 1 || page > 1000 {
				communityError(c, 400, "invalid_request")
				return
			}
		}
		if q.Has("past") && q.Get("past") != "1" {
			communityError(c, 400, "invalid_request")
			return
		}
		var cmd events.Command
		if op == "command" {
			if !chatJSON(c, &cmd) || len(c.Request.Header.Values("Idempotency-Key")) != 1 {
				communityError(c, 400, "invalid_request")
				return
			}
			if admin != (cmd.Op == "create" || cmd.Op == "edit" || cmd.Op == "cancel") {
				communityError(c, 403, "forbidden")
				return
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
		defer cancel()
		store, err := uc.communityEventStore(ctx)
		if err != nil {
			communityError(c, 503, "events_unavailable")
			return
		}
		var result any
		switch op {
		case "list":
			result, err = store.List(ctx, user, page, q.Get("past") == "1")
		case "roster":
			result, err = store.Roster(ctx, user, q.Get("id"), page)
		case "command":
			result, err = store.Execute(ctx, user, c.GetHeader("Idempotency-Key"), cmd)
		}
		if err != nil {
			status := 503
			code := "events_unavailable"
			switch {
			case errors.Is(err, events.ErrLimit):
				status = 429
				code = "rate_limited"
			case errors.Is(err, events.ErrForbidden):
				status = 403
				code = "forbidden"
			case errors.Is(err, events.ErrInvalid):
				status = 400
				code = "invalid_request"
			case errors.Is(err, events.ErrConflict), errors.Is(err, events.ErrClosed), errors.Is(err, events.ErrFull):
				status = 409
				code = err.Error()
			}
			communityError(c, status, code)
			return
		}
		c.JSON(200, gin.H{"code": 200, "data": result})
	}
}

package pulse_user_center

import (
	"context"
	"database/sql"
	"errors"
	"github.com/gin-gonic/gin"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/awards"
	"net/url"
	"os"
	"strconv"
	"time"
)

func (uc *UserCenter) awardStore(ctx context.Context) (*awards.Store, error) {
	uc.awardMu.Lock()
	defer uc.awardMu.Unlock()
	if uc.awards != nil {
		return uc.awards, nil
	}
	cfg, err := mysql.ParseDSN(os.Getenv("FORUM_BINDING_GUARD_DSN"))
	if err != nil || cfg.DBName == "" {
		return nil, errors.New("awards database unavailable")
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
	store := awards.New(db)
	if err = store.Ensure(ctx); err != nil {
		db.Close()
		return nil, err
	}
	uc.awards = store
	return store, nil
}

func (uc *UserCenter) registerAwards(r *gin.RouterGroup, session func(*gin.Context) (string, error), admin bool, site func() string) {
	if session == nil {
		r.GET("/metar/awards/profile", uc.awardHandler("profile", nil, false, site))
		r.GET("/metar/awards", uc.awardHandler("list", nil, false, site))
		return
	}
	if admin {
		r.GET("/metar/awards", uc.awardHandler("list", session, true, site))
		r.GET("/metar/awards/audit", uc.awardHandler("audit", session, true, site))
	} else {
		r.GET("/metar/awards/honors", uc.awardHandler("honors", session, false, site))
		r.GET("/metar/awards/submissions", uc.awardHandler("submissions", session, false, site))
		r.GET("/metar/awards/status", uc.awardHandler("status", session, false, site))
	}
	r.POST("/metar/awards/command", uc.awardHandler("command", session, admin, site))
}
func (uc *UserCenter) awardHandler(op string, session func(*gin.Context) (string, error), admin bool, site func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
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
		q, err := url.ParseQuery(c.Request.URL.RawQuery)
		if err != nil {
			communityError(c, 400, "invalid_request")
			return
		}
		for k, v := range q {
			if len(v) != 1 || !(op == "submissions" && (k == "key" || k == "type") || op == "profile" && k == "username" || op == "honors" && k == "page" || op == "status" && k == "question_id" || (op == "list" || op == "audit") && (k == "type" || k == "key" || k == "kind" || k == "page")) {
				communityError(c, 400, "invalid_request")
				return
			}
		}
		p := awards.Period{Type: q.Get("type"), Key: q.Get("key"), Kind: q.Get("kind")}
		page := 1
		var cmd awards.Command
		if op == "profile" && q.Get("username") == "" {
			communityError(c, 400, "invalid_request")
			return
		}
		if op == "honors" && q.Has("page") {
			page, err = strconv.Atoi(q.Get("page"))
			if err != nil || page < 1 || page > 10000 {
				communityError(c, 400, "invalid_request")
				return
			}
		}
		if op == "submissions" {
			p.Kind = "post"
		}
		if op == "list" || op == "audit" || op == "submissions" {
			if _, err = p.Start(); err != nil {
				communityError(c, 400, "invalid_request")
				return
			}
			if q.Has("page") {
				page, err = strconv.Atoi(q.Get("page"))
				if err != nil || page < 1 || page > 10000 {
					communityError(c, 400, "invalid_request")
					return
				}
			}
		}
		id := ""
		if op == "status" {
			id = pollQuestionID(q.Get("question_id"))
			if id == "" {
				communityError(c, 400, "invalid_request")
				return
			}
		}
		if op == "command" {
			if !communitySameOrigin(c.Request, site()) || c.GetHeader("X-Metar-Request") != "1" {
				communityError(c, 403, "origin_rejected")
				return
			}
			if !experienceJSON(c, &cmd) || len(c.Request.Header.Values("Idempotency-Key")) != 1 {
				communityError(c, 400, "invalid_request")
				return
			}
			if cmd.Op == "nominate" {
				cmd.QuestionID = pollQuestionID(cmd.QuestionID)
			}
			if cmd.Validate() != nil {
				communityError(c, 400, "invalid_request")
				return
			}
			if admin != (cmd.Op != "withdraw" && cmd.Op != "nominate" && cmd.Op != "title") {
				communityError(c, 403, "forbidden")
				return
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer cancel()
		store, err := uc.awardStore(ctx)
		if err != nil {
			communityError(c, 503, "awards_unavailable")
			return
		}
		var result any
		switch op {
		case "submissions":
			result, err = store.Submissions(ctx, user, p)
		case "profile":
			result, err = store.Honors(ctx, "", q.Get("username"), 1)
		case "honors":
			result, err = store.Honors(ctx, user, "", page)
		case "list":
			result, err = store.List(ctx, user, p, page, admin)
		case "audit":
			result, err = store.Audit(ctx, user, p)
		case "status":
			result, err = store.Status(ctx, user, id)
		case "command":
			result, err = store.Execute(ctx, user, c.GetHeader("Idempotency-Key"), cmd, admin)
		}
		if err != nil {
			status, code := 503, "awards_unavailable"
			switch {
			case errors.Is(err, awards.ErrInvalid):
				status, code = 400, "invalid_request"
			case errors.Is(err, awards.ErrForbidden):
				status, code = 403, "forbidden"
			case errors.Is(err, awards.ErrNotFound):
				status, code = 404, "not_found"
			case errors.Is(err, awards.ErrClosed):
				status, code = 409, "period_closed"
			case errors.Is(err, awards.ErrRecommendationLimit):
				status, code = 409, "recommendation_limit"
			case errors.Is(err, awards.ErrQuota):
				status, code = 409, "nomination_limit"
			case errors.Is(err, awards.ErrConflict):
				status, code = 409, "conflict"
			case errors.Is(err, awards.ErrNotReady):
				status, code = 409, "review_not_ready"
			}
			communityError(c, status, code)
			return
		}
		c.JSON(200, gin.H{"code": 200, "data": result})
	}
}

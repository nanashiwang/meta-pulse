package pulse_user_center

import (
	"context"
	"database/sql"
	"errors"
	"github.com/apache/answer/pkg/uid"
	"github.com/gin-gonic/gin"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/polls"
	"log"
	"net/url"
	"os"
	"time"
)

func (uc *UserCenter) pollStore(ctx context.Context) (*polls.Store, error) {
	uc.pollMu.Lock()
	defer uc.pollMu.Unlock()
	if uc.polls != nil {
		return uc.polls, nil
	}
	cfg, err := mysql.ParseDSN(os.Getenv("FORUM_BINDING_GUARD_DSN"))
	if err != nil || cfg.DBName == "" {
		return nil, errors.New("poll database unavailable")
	}
	if cfg.TLSConfig == "skip-verify" || cfg.TLSConfig == "preferred" {
		return nil, errors.New("verified TLS required")
	}
	cfg.MultiStatements = false
	cfg.ParseTime = true
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 10 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 10 * time.Second
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	store := polls.New(db)
	if err = store.Ensure(ctx); err != nil {
		db.Close()
		return nil, err
	}
	uc.polls = store
	return store, nil
}
func pollQuestionID(id string) string {
	if len(id) == 0 || len(id) > 24 {
		return ""
	}
	decoded := uid.DeShortID(id)
	if !canonicalCommunityID(decoded) {
		return ""
	}
	if decoded != id && uid.EnShortID(decoded) != id {
		return ""
	}
	return decoded
}
func (uc *UserCenter) registerPolls(r *gin.RouterGroup, session func(*gin.Context) (string, error), site func() string) {
	if session == nil {
		r.GET("/metar/polls", uc.pollHandler(nil, site, false))
		return
	}
	r.GET("/metar/polls/voters", uc.pollVotersHandler(session))
	r.GET("/metar/polls/mine", uc.pollHandler(session, site, false))
	r.POST("/metar/polls/command", uc.pollHandler(session, site, true))
}
func (uc *UserCenter) pollHandler(session func(*gin.Context) (string, error), site func() string, write bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		q, err := url.ParseQuery(c.Request.URL.RawQuery)
		if err != nil || (write && len(q) != 0) || (!write && (len(q) != 1 || len(q["question_id"]) != 1)) {
			communityError(c, 400, "invalid_request")
			return
		}
		user := ""
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
		var command polls.Command
		id := ""
		if write {
			if !communitySameOrigin(c.Request, site()) || c.GetHeader("X-Metar-Request") != "1" {
				communityError(c, 403, "origin_rejected")
				return
			}
			if !experienceJSON(c, &command) || len(c.Request.Header.Values("Idempotency-Key")) != 1 {
				communityError(c, 400, "invalid_request")
				return
			}
			command.QuestionID = pollQuestionID(command.QuestionID)
			if _, err = polls.Normalize(command); err != nil {
				communityError(c, 400, "invalid_request")
				return
			}
		} else {
			id = pollQuestionID(q.Get("question_id"))
			if id == "" {
				communityError(c, 400, "invalid_request")
				return
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		store, err := uc.pollStore(ctx)
		if err != nil {
			communityError(c, 503, "poll_unavailable")
			return
		}
		var result any
		if write {
			result, err = store.Execute(ctx, user, c.GetHeader("Idempotency-Key"), command)
		} else {
			result, err = store.Get(ctx, id, user)
		}
		if err != nil {
			status, code := 503, "poll_unavailable"
			switch {
			case errors.Is(err, polls.ErrInvalid):
				status, code = 400, "invalid_request"
			case errors.Is(err, polls.ErrForbidden):
				status, code = 403, "forbidden"
			case errors.Is(err, polls.ErrNotFound):
				status, code = 404, "not_found"
			case errors.Is(err, polls.ErrConflict):
				status, code = 409, "conflict"
			case errors.Is(err, polls.ErrClosed):
				status, code = 409, "poll_closed"
			}
			communityError(c, status, code)
			return
		}
		c.JSON(200, gin.H{"data": result})
	}
}

func (uc *UserCenter) pollVotersHandler(session func(*gin.Context) (string, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, private")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Vary", "Authorization")
		q, err := url.ParseQuery(c.Request.URL.RawQuery)
		if err != nil || len(q["question_id"]) != 1 || len(q) > 2 || len(q["after"]) > 1 || (len(q) == 2 && len(q["after"]) != 1) {
			communityError(c, 400, "invalid_request")
			return
		}
		if session == nil || c.GetHeader("Authorization") == "" {
			communityError(c, 401, "authentication_required")
			return
		}
		user, err := session(c)
		if err != nil {
			communityIdentityError(c, err)
			return
		}
		id := pollQuestionID(q.Get("question_id"))
		after := q.Get("after")
		if id == "" || (after != "" && !canonicalCommunityID(after)) {
			communityError(c, 400, "invalid_request")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		store, err := uc.pollStore(ctx)
		if err != nil {
			communityError(c, 503, "poll_unavailable")
			return
		}
		page, err := store.Voters(ctx, id, user, after)
		if err != nil {
			switch {
			case errors.Is(err, polls.ErrForbidden):
				communityError(c, 403, "forbidden")
			case errors.Is(err, polls.ErrNotFound):
				communityError(c, 404, "not_found")
			case errors.Is(err, polls.ErrInvalid):
				communityError(c, 400, "invalid_request")
			default:
				communityError(c, 503, "poll_unavailable")
			}
			return
		}
		log.Printf("poll voter inspection admin=%s question=%s count=%d", user, id, len(page.Items))
		c.JSON(200, gin.H{"code": 200, "data": page})
	}
}

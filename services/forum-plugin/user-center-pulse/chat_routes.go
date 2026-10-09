package pulse_user_center

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/chat"
)

// Chat has a bounded independent pool and never initializes Pulse or EXP.
func (uc *UserCenter) conversationStore(ctx context.Context) (*chat.Store, error) {
	uc.chatMu.Lock()
	defer uc.chatMu.Unlock()
	if uc.chatStore != nil {
		return uc.chatStore, nil
	}
	cfg, err := mysql.ParseDSN(os.Getenv("FORUM_BINDING_GUARD_DSN"))
	if err != nil || cfg.DBName == "" {
		return nil, errors.New("chat database unavailable")
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
	store := chat.New(db)
	if err = store.Ensure(ctx); err != nil {
		db.Close()
		return nil, err
	}
	uc.chatStore = store
	return store, nil
}
func (uc *UserCenter) registerChat(r *gin.RouterGroup, session func(*gin.Context) (string, error), isAdmin bool, siteURL func() string) {
	if r == nil {
		return
	}
	if isAdmin {
		r.GET("/metar/chat/reports", uc.chatHandler("reports", session, true, siteURL))
		r.POST("/metar/chat/reports", uc.chatHandler("command", session, true, siteURL))
		return
	}
	for _, op := range []string{"summary", "inbox", "messages", "people", "blocks"} {
		r.GET("/metar/chat/"+op, uc.chatHandler(op, session, false, siteURL))
	}
	r.POST("/metar/chat/command", uc.chatHandler("command", session, false, siteURL))
}
func chatJSON(c *gin.Context, v any) bool {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 32769))
	if err != nil || len(raw) > 32768 || !uniqueExperienceJSON(raw) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(v) == nil
}
func (uc *UserCenter) chatHandler(op string, session func(*gin.Context) (string, error), isAdmin bool, siteURL func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Robots-Tag", "noindex, nofollow")
		if c.GetHeader("Authorization") == "" {
			communityError(c, 401, "authentication_required")
			return
		}
		user, err := session(c)
		if err != nil {
			communityIdentityError(c, err)
			return
		}
		if c.Request.Method != "GET" && !communitySameOrigin(c.Request, siteURL()) {
			communityError(c, 403, "origin_rejected")
			return
		}
		q, err := url.ParseQuery(c.Request.URL.RawQuery)
		allowed := map[string]bool{}
		switch op {
		case "inbox", "reports":
			allowed["before"] = true
		case "messages":
			allowed["room_id"] = true
			allowed["before"] = true
			allowed["after"] = true
			allowed["version"] = true
		case "people":
			allowed["q"] = true
		}
		invalid := err != nil
		for k, v := range q {
			if !allowed[k] || len(v) != 1 {
				invalid = true
			}
		}
		if invalid {
			communityError(c, 400, "invalid_request")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
		defer cancel()
		store, err := uc.conversationStore(ctx)
		if err != nil {
			communityError(c, 503, "chat_unavailable")
			return
		}
		var result any
		switch op {
		case "summary":
			result, err = store.Summary(ctx, user)
		case "inbox":
			result, err = store.Inbox(ctx, user, q.Get("before"))
		case "people":
			result, err = store.People(ctx, user, q.Get("q"))
		case "blocks":
			result, err = store.Blocks(ctx, user)
		case "reports":
			result, err = store.Reports(ctx, user, q.Get("before"))
		case "messages":
			var before, after int64
			for name, dst := range map[string]*int64{"before": &before, "after": &after} {
				if v := q.Get(name); v != "" {
					n, e := strconv.ParseInt(v, 10, 64)
					if e != nil || n <= 0 || strconv.FormatInt(n, 10) != v {
						err = chat.ErrInvalid
						break
					}
					*dst = n
				}
			}
			if err == nil {
				if q.Has("version") {
					version, e := strconv.ParseInt(q.Get("version"), 10, 64)
					if e != nil || version < 0 || strconv.FormatInt(version, 10) != q.Get("version") || before != 0 || after != 0 {
						err = chat.ErrInvalid
					} else {
						result, err = store.Changes(ctx, user, q.Get("room_id"), version)
					}
				} else {
					result, err = store.History(ctx, user, q.Get("room_id"), before, after)
				}
			}
		case "command":
			var cmd chat.Command
			if !chatJSON(c, &cmd) || len(c.Request.Header.Values("Idempotency-Key")) != 1 {
				err = chat.ErrInvalid
				break
			}
			if isAdmin != (cmd.Op == "resolve_report") {
				err = chat.ErrForbidden
				break
			}
			result, err = store.Execute(ctx, user, c.GetHeader("Idempotency-Key"), cmd)
		}
		if err != nil {
			status, code := 503, "chat_unavailable"
			switch {
			case errors.Is(err, chat.ErrInvalid):
				status, code = 400, "invalid_request"
			case errors.Is(err, chat.ErrConflict):
				status, code = 409, "conflict"
			case errors.Is(err, chat.ErrUnavailable):
				status, code = 403, "account_unavailable"
			case chat.IsDenied(err):
				status, code = 403, "forbidden"
			case errors.Is(err, chat.ErrBlocked):
				status, code = 403, "contact_unavailable"
			case errors.Is(err, chat.ErrLimit):
				status, code = 429, "rate_limited"
				c.Header("Retry-After", "60")
			}
			communityError(c, status, code)
			return
		}
		c.JSON(200, gin.H{"data": result})
	}
}

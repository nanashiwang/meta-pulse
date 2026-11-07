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
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/marketplace"
)

// Independent Answer pool: no Pulse, new-api, optional binding or money writes.
func (uc *UserCenter) marketplaceStore(ctx context.Context) (*marketplace.Store, error) {
	uc.marketMu.Lock()
	defer uc.marketMu.Unlock()
	if uc.marketStore != nil {
		return uc.marketStore, nil
	}
	cfg, err := mysqlDriver.ParseDSN(os.Getenv("FORUM_BINDING_GUARD_DSN"))
	if err != nil || cfg.DBName == "" {
		return nil, errors.New("marketplace database unavailable")
	}
	if cfg.TLSConfig == "skip-verify" || cfg.TLSConfig == "preferred" {
		return nil, errors.New("unverified Forum TLS")
	}
	cfg.MultiStatements = false
	cfg.ParseTime = true
	cfg.Timeout = 3 * time.Second
	cfg.ReadTimeout = 12 * time.Second
	cfg.WriteTimeout = 12 * time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	s := marketplace.New(db)
	if err = s.Ensure(ctx); err != nil {
		db.Close()
		return nil, err
	}
	uc.marketStore = s
	return s, nil
}
func (uc *UserCenter) registerMarketplacePublic(r *gin.RouterGroup) {
	r.GET("/metar/marketplace/items", uc.marketplaceHandler("list", nil, false, nil))
	r.GET("/metar/marketplace/items/:id", uc.marketplaceHandler("detail", nil, false, nil))
}
func (uc *UserCenter) registerMarketplaceUser(r *gin.RouterGroup, session func(*gin.Context) (string, error), site func() string) {
	r.GET("/metar/marketplace/mine", uc.marketplaceHandler("mine", session, false, site))
	r.POST("/metar/marketplace/command", uc.marketplaceHandler("command", session, false, site))
}
func (uc *UserCenter) registerMarketplaceAdmin(r *gin.RouterGroup, session func(*gin.Context) (string, error), site func() string) {
	r.GET("/metar/marketplace/admin/items", uc.marketplaceHandler("mine", session, true, site))
	r.POST("/metar/marketplace/admin/command", uc.marketplaceHandler("command", session, true, site))
}
func (uc *UserCenter) marketplaceHandler(op string, session func(*gin.Context) (string, error), admin bool, site func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Robots-Tag", "noindex, nofollow")
		q, err := url.ParseQuery(c.Request.URL.RawQuery)
		allowed := map[string]bool{}
		switch op {
		case "list":
			for _, k := range []string{"kind", "q", "page", "use_case", "capability", "order"} {
				allowed[k] = true
			}
		case "mine":
			allowed["page"] = true
			if admin {
				allowed["status"] = true
			}
		}
		if err != nil {
			communityError(c, 400, "invalid_request")
			return
		}
		for k, v := range q {
			if !allowed[k] || len(v) != 1 {
				communityError(c, 400, "invalid_request")
				return
			}
		}
		page := 1
		if q.Has("page") {
			page, err = strconv.Atoi(q.Get("page"))
			if err != nil || page < 1 || page > 1000 || strconv.Itoa(page) != q.Get("page") {
				communityError(c, 400, "invalid_request")
				return
			}
		}
		query := marketplace.Query{Kind: q.Get("kind"), Text: strings.TrimSpace(q.Get("q")), Page: page, UseCase: q.Get("use_case"), Capability: q.Get("capability"), Order: q.Get("order"), Status: q.Get("status")}
		if err = query.Validate(admin); err != nil {
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
		var cmd marketplace.Command
		if op == "command" {
			if !communitySameOrigin(c.Request, site()) || c.GetHeader("X-Metar-Request") != "1" {
				communityError(c, 403, "origin_rejected")
				return
			}
			if len(c.Request.Header.Values("Idempotency-Key")) != 1 || !communityRequestID.MatchString(c.GetHeader("Idempotency-Key")) || !marketplaceJSON(c, &cmd) {
				communityError(c, 400, "invalid_request")
				return
			}
			if admin != (cmd.Op == "review") {
				communityError(c, 403, "forbidden")
				return
			}
			// Runtime addresses must stay outside the community's origin.
			if cmd.Entry.DemoURL != "" {
				demo, e := url.Parse(cmd.Entry.DemoURL)
				origin, o := url.Parse(site())
				if e != nil || o != nil || strings.EqualFold(demo.Hostname(), origin.Hostname()) {
					communityError(c, 400, "invalid_request")
					return
				}
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
		defer cancel()
		s, err := uc.marketplaceStore(ctx)
		if err != nil {
			communityError(c, 503, "marketplace_unavailable")
			return
		}
		var result any
		switch op {
		case "list":
			result, err = s.List(ctx, query)
		case "detail":
			result, err = s.Detail(ctx, c.Param("id"))
		case "mine":
			result, err = s.PrivateList(ctx, user, query, admin)
		case "command":
			result, err = s.Execute(ctx, user, c.GetHeader("Idempotency-Key"), cmd, admin)
		}
		if err != nil {
			marketplaceError(c, err)
			return
		}
		c.JSON(200, gin.H{"data": result})
	}
}
func marketplaceJSON(c *gin.Context, v any) bool {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 16385))
	if err != nil || len(raw) > 16384 || !uniqueExperienceJSON(raw) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(v) == nil && d.Decode(new(any)) == io.EOF
}
func marketplaceError(c *gin.Context, err error) {
	status, code := 503, "marketplace_unavailable"
	switch {
	case errors.Is(err, marketplace.ErrInvalid):
		status, code = 400, "invalid_request"
	case errors.Is(err, marketplace.ErrForbidden):
		status, code = 403, "forbidden"
	case errors.Is(err, marketplace.ErrConflict):
		status, code = 409, "conflict"
	case errors.Is(err, marketplace.ErrNotFound):
		status, code = 404, "not_found"
	case errors.Is(err, marketplace.ErrLimit):
		status, code = 429, "rate_limited"
	}
	communityError(c, status, code)
}

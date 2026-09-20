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
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/growth"
	"github.com/segmentfault/pacman/log"
)

// Experience uses a separate pool and does not depend on Pulse connectivity,
// Redis, optional account binding, or connector configuration refreshes.
func (uc *UserCenter) experienceStore(ctx context.Context) (*growth.Store, error) {
	uc.growthMu.Lock()
	defer uc.growthMu.Unlock()
	if uc.growthStore != nil {
		return uc.growthStore, nil
	}
	cfg, err := mysqlDriver.ParseDSN(os.Getenv("FORUM_BINDING_GUARD_DSN"))
	if err != nil || cfg.DBName == "" {
		return nil, errors.New("experience database unavailable")
	}
	cfg.MultiStatements = false
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(6)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	store := growth.New(db)
	if err = store.Ensure(ctx); err != nil {
		db.Close()
		return nil, err
	}
	uc.growthStore = store
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			c, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			err := store.Collect(c)
			cancel()
			if err != nil {
				log.Warnf("community experience collection failed: %v", err)
			}
			<-ticker.C
		}
	}()
	return store, nil
}
func (uc *UserCenter) registerExperience(r *gin.RouterGroup, session func(*gin.Context) (string, error), admin bool, siteURL func() string) {
	if r == nil {
		return
	}
	operations := map[string]string{"summary": "GET", "history": "GET", "pending": "GET", "checkin": "POST", "appearance": "POST", "notices/read": "POST"}
	if admin {
		operations = map[string]string{"settings": "GET", "rules": "PUT", "adjustment": "POST", "featured": "POST"}
	}
	for operation, method := range operations {
		r.Handle(method, "/metar/experience/"+operation, uc.experienceHandler(operation, session, admin, siteURL))
	}
}
func experienceJSON(c *gin.Context, v any) bool {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 8193))
	if err != nil || len(raw) > 8192 {
		return false
	}
	if !uniqueExperienceJSON(raw) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(v) == nil
}
func (uc *UserCenter) experienceHandler(operation string, session func(*gin.Context) (string, error), admin bool, siteURL func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		q, queryErr := url.ParseQuery(c.Request.URL.RawQuery)
		if queryErr != nil || len(q) > 0 && (operation != "history" || len(q) != 1 || len(q["before"]) != 1) {
			communityError(c, 400, "invalid_request")
			return
		}
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
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		store, err := uc.experienceStore(ctx)
		if err != nil {
			communityError(c, 503, "experience_unavailable")
			return
		}
		if admin {
			if err = store.CheckAdmin(ctx, user); err != nil {
				communityError(c, 403, "forbidden")
				return
			}
		}
		var result any = gin.H{"ok": true}
		switch operation {
		case "summary":
			result, err = store.Summary(ctx, user)
		case "history":
			var before uint64
			if q.Get("before") != "" {
				before, err = strconv.ParseUint(q.Get("before"), 10, 64)
				if err != nil || before == 0 {
					err = growth.ErrInvalid
				}
			}
			if err == nil {
				result, err = store.History(ctx, user, before)
			}
		case "pending":
			result, err = store.Pending(ctx, user)
		case "checkin", "notices/read":
			var body struct{}
			if !experienceJSON(c, &body) {
				err = growth.ErrInvalid
				break
			}
			if operation == "checkin" {
				err = store.Checkin(ctx, user)
				if err == nil {
					result, err = store.Summary(ctx, user)
				}
			} else {
				err = store.ReadNotices(ctx, user)
			}
		case "appearance":
			var body struct {
				Appearance string `json:"appearance"`
			}
			if !experienceJSON(c, &body) {
				err = growth.ErrInvalid
				break
			}
			err = store.Appearance(ctx, user, body.Appearance)
		case "settings":
			result, err = store.Settings(ctx)
		default:
			key := c.GetHeader("Idempotency-Key")
			if !communityRequestID.MatchString(key) || len(c.Request.Header.Values("Idempotency-Key")) != 1 {
				err = growth.ErrInvalid
				break
			}
			switch operation {
			case "rules":
				var body struct {
					Version int64        `json:"version"`
					Rules   growth.Rules `json:"rules"`
					Reason  string       `json:"reason"`
				}
				if !experienceJSON(c, &body) {
					err = growth.ErrInvalid
					break
				}
				err = store.UpdateRules(ctx, user, key, body.Reason, body.Version, body.Rules)
			case "adjustment":
				var body struct {
					UserID string `json:"user_id"`
					Delta  int64  `json:"delta"`
					Reason string `json:"reason"`
				}
				if !experienceJSON(c, &body) {
					err = growth.ErrInvalid
					break
				}
				err = store.Adjust(ctx, user, key, body.UserID, body.Reason, body.Delta)
			case "featured":
				var body struct {
					ObjectType string `json:"object_type"`
					ObjectID   string `json:"object_id"`
					Reason     string `json:"reason"`
				}
				if !experienceJSON(c, &body) {
					err = growth.ErrInvalid
					break
				}
				err = store.Feature(ctx, user, key, body.ObjectType, body.ObjectID, body.Reason)
			}
		}
		if err != nil {
			status, code := 503, "experience_unavailable"
			switch {
			case errors.Is(err, growth.ErrInvalid):
				status, code = 400, "invalid_request"
			case errors.Is(err, growth.ErrConflict):
				status, code = 409, "conflict"
			case errors.Is(err, growth.ErrForbidden):
				status, code = 403, "account_unavailable"
			case errors.Is(err, sql.ErrNoRows):
				status, code = 404, "not_found"
			}
			communityError(c, status, code)
			return
		}
		c.JSON(200, gin.H{"data": result})
	}
}
func (uc *UserCenter) registerPublicExperience(r *gin.RouterGroup) {
	if r == nil {
		return
	}
	r.GET("/metar/experience/profile", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		q, queryErr := url.ParseQuery(c.Request.URL.RawQuery)
		if queryErr != nil || len(q) != 1 || len(q["username"]) != 1 || len(q.Get("username")) < 1 || len(q.Get("username")) > 60 {
			communityError(c, 400, "invalid_request")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		s, err := uc.experienceStore(ctx)
		if err != nil {
			communityError(c, 503, "experience_unavailable")
			return
		}
		result, err := s.Public(ctx, q.Get("username"))
		if err != nil {
			communityError(c, 404, "not_found")
			return
		}
		c.JSON(200, gin.H{"data": result})
	})
}

// Reject duplicate keys at every nesting level so audit and validation cannot
// interpret a payload differently. Rules are nested objects.
func uniqueExperienceJSON(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func() bool
	walk = func() bool {
		token, err := d.Token()
		if err != nil {
			return false
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				name, ok := k.(string)
				if e != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !walk() {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !walk() {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' || !walk() {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

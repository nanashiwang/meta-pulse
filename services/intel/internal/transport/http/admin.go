package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/admin"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/catalog"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/security"
	mysqlstore "github.com/nanashiwang/meta-pulse/services/intel/internal/store/mysql"
)

// MaxAdminBody bounds documents sent through the browser admin; it matches
// the gateway's 1 MiB body limit. Larger imports use metar-intel-tool.
const MaxAdminBody = 1 << 20

// SourceAdmin manages data-source credentials. Optional: nil leaves the
// /sources routes unregistered.
type SourceAdmin interface {
	View(ctx context.Context) (admin.SourcesView, error)
	UpdateLLMStats(ctx context.Context, actor string, req admin.LLMStatsUpdate) (admin.SourcesView, error)
	RequestSync(ctx context.Context, actor string) (admin.SourcesView, error)
}

type Admin interface {
	Overview(ctx context.Context) (mysqlstore.Overview, error)
	Export(ctx context.Context) (catalog.Document, error)
	Validate(ctx context.Context, doc catalog.Document) (map[string]int, error)
	Import(ctx context.Context, doc catalog.Document, raw []byte, actorID, key string) (admin.ImportResult, error)
	Recompute(ctx context.Context) (admin.SnapshotResult, error)
}

var idempotencyKeyPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// RegisterAdmin adds the signed admin routes. Without at least one usable
// secret nothing is registered, so a misconfigured deployment exposes no
// admin surface at all.
func RegisterAdmin(r *gin.Engine, logger *slog.Logger, svc Admin, sources SourceAdmin, secrets [][]byte, nonces security.NonceStore, now func() time.Time) bool {
	usable := false
	for _, s := range secrets {
		if len(s) >= 32 {
			usable = true
		}
	}
	if !usable || svc == nil || nonces == nil {
		return false
	}
	if logger == nil {
		logger = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	g := r.Group("/v1/internal/admin", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if c.Request.URL.RawQuery != "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		actor, body, err := security.Verify(c.Request, secrets, now(), MaxAdminBody, nonces)
		if err != nil {
			if !errors.Is(err, security.ErrInvalid) && !errors.Is(err, security.ErrReplay) {
				logger.Error("intel admin nonce store failed", "error", err)
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
				return
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set("intel.actor", actor)
		c.Set("intel.body", body)
		c.Next()
	})
	fail := func(c *gin.Context, err error) {
		var invalid *catalog.ValidationError
		switch {
		case errors.As(err, &invalid):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_document", "problems": invalid.Problems})
		case errors.Is(err, mysqlstore.ErrIdempotencyConflict):
			c.JSON(http.StatusConflict, gin.H{"error": "idempotency_conflict"})
		case errors.Is(err, admin.ErrInvalidAPIKey):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_api_key"})
		case errors.Is(err, admin.ErrManagedByEnv):
			c.JSON(http.StatusConflict, gin.H{"error": "managed_by_env"})
		case errors.Is(err, admin.ErrNotConfigured):
			c.JSON(http.StatusConflict, gin.H{"error": "not_configured"})
		case errors.Is(err, admin.ErrInvalidSourceReq):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		case errors.Is(err, admin.ErrVerifyFailed):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "verify_unavailable"})
		default:
			logger.Error("intel admin operation failed", "path", c.FullPath(), "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		}
	}
	document := func(c *gin.Context) (catalog.Document, []byte, bool) {
		raw := c.MustGet("intel.body").([]byte)
		doc, err := catalog.DecodeDocument(raw)
		if err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_document", "problems": []string{err.Error()}})
			return doc, nil, false
		}
		return doc, raw, true
	}

	g.GET("/overview", func(c *gin.Context) {
		o, err := svc.Overview(c.Request.Context())
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(http.StatusOK, o)
	})
	g.GET("/export", func(c *gin.Context) {
		d, err := svc.Export(c.Request.Context())
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(http.StatusOK, d)
	})
	g.POST("/validate", func(c *gin.Context) {
		doc, _, ok := document(c)
		if !ok {
			return
		}
		summary, err := svc.Validate(c.Request.Context(), doc)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"valid": true, "summary": summary})
	})
	g.POST("/import", func(c *gin.Context) {
		key := c.GetHeader("Idempotency-Key")
		if !idempotencyKeyPattern.MatchString(key) || len(c.Request.Header.Values("Idempotency-Key")) != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_idempotency_key"})
			return
		}
		doc, raw, ok := document(c)
		if !ok {
			return
		}
		res, err := svc.Import(c.Request.Context(), doc, raw, c.GetString("intel.actor"), key)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(http.StatusOK, res)
	})
	if sources != nil {
		g.GET("/sources", func(c *gin.Context) {
			v, err := sources.View(c.Request.Context())
			if err != nil {
				fail(c, err)
				return
			}
			c.JSON(http.StatusOK, v)
		})
		g.PUT("/sources/llmstats", func(c *gin.Context) {
			var req admin.LLMStatsUpdate
			dec := json.NewDecoder(bytes.NewReader(c.MustGet("intel.body").([]byte)))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
				return
			}
			v, err := sources.UpdateLLMStats(c.Request.Context(), c.GetString("intel.actor"), req)
			if err != nil {
				fail(c, err)
				return
			}
			c.JSON(http.StatusOK, v)
		})
		g.POST("/sources/llmstats/sync", func(c *gin.Context) {
			if len(c.MustGet("intel.body").([]byte)) != 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
				return
			}
			v, err := sources.RequestSync(c.Request.Context(), c.GetString("intel.actor"))
			if err != nil {
				fail(c, err)
				return
			}
			c.JSON(http.StatusAccepted, v)
		})
	}
	g.POST("/recompute", func(c *gin.Context) {
		if len(c.MustGet("intel.body").([]byte)) != 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		res, err := svc.Recompute(c.Request.Context())
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(http.StatusOK, res)
	})
	return true
}

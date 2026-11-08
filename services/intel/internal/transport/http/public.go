// Package http exposes Intel's anonymous, read-only API. There are no write
// routes and no cookies: nginx forwards GET/HEAD only and strips credentials.
package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/service"
)

type Reader interface {
	Home(ctx context.Context) (service.Home, error)
	Leaderboard(ctx context.Context, q service.Query) (service.Board, error)
	Model(ctx context.Context, slug string) (service.ModelDetail, error)
	Methodology(ctx context.Context, speedWindow time.Duration) (service.Methodology, error)
}

type Pinger interface {
	Ping(ctx context.Context) error
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

// publicCache lets nginx and browsers absorb load; the board changes at most
// once per worker interval.
const publicCache = "public, max-age=60, stale-while-revalidate=300"

func NewRouter(logger *slog.Logger, reader Reader, db Pinger, speedWindow time.Duration) *gin.Engine {
	if logger == nil {
		logger = slog.Default()
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), requestLogger(logger), securityHeaders)
	r.HandleMethodNotAllowed = true

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "metar-intel-api"}) })
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if db == nil || db.Ping(ctx) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := r.Group("/v1/public")
	v1.GET("/home", func(c *gin.Context) {
		home, err := reader.Home(c.Request.Context())
		respond(c, logger, home, err)
	})
	v1.GET("/leaderboard", func(c *gin.Context) {
		q, ok := parseQuery(c)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_query"})
			return
		}
		board, err := reader.Leaderboard(c.Request.Context(), q)
		respond(c, logger, board, err)
	})
	v1.GET("/models/:slug", func(c *gin.Context) {
		slug := c.Param("slug")
		if len(slug) > 80 || !slugPattern.MatchString(slug) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		d, err := reader.Model(c.Request.Context(), slug)
		respond(c, logger, d, err)
	})
	v1.GET("/methodology", func(c *gin.Context) {
		m, err := reader.Methodology(c.Request.Context(), speedWindow)
		respond(c, logger, m, err)
	})
	r.NoRoute(func(c *gin.Context) { c.JSON(http.StatusNotFound, gin.H{"error": "not_found"}) })
	r.NoMethod(func(c *gin.Context) { c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "method_not_allowed"}) })
	return r
}

// parseQuery accepts only known parameters with exact values; anything else
// is rejected instead of silently ignored, so cached URLs stay canonical.
func parseQuery(c *gin.Context) (service.Query, bool) {
	allowed := map[string]bool{"window": true, "sort": true, "order": true, "open": true, "page": true}
	values := c.Request.URL.Query()
	for k, v := range values {
		if !allowed[k] || len(v) != 1 {
			return service.Query{}, false
		}
	}
	q := service.Query{Window: values.Get("window"), Sort: values.Get("sort")}
	switch values.Get("order") {
	case "", "desc":
	case "asc":
		q.Asc = true
	default:
		return q, false
	}
	switch values.Get("open") {
	case "", "0":
	case "1":
		q.OpenOnly = true
	default:
		return q, false
	}
	if p := values.Get("page"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 {
			return q, false
		}
		q.Page = n
	}
	return q, q.Normalize() == nil
}

func respond(c *gin.Context, logger *slog.Logger, body any, err error) {
	switch {
	case err == nil:
		c.Header("Cache-Control", publicCache)
		c.JSON(http.StatusOK, body)
	case errors.Is(err, service.ErrBadQuery):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_query"})
	case errors.Is(err, service.ErrNotFound):
		c.Header("Cache-Control", "public, max-age=60")
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
	case errors.Is(err, service.ErrNoSnapshot):
		// Distinct from an outage: the service works but nothing is imported yet.
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusNotFound, gin.H{"error": "no_snapshot"})
	default:
		logger.Error("intel public read failed", "path", c.FullPath(), "error", err)
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
	}
}

func securityHeaders(c *gin.Context) {
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "no-referrer")
	c.Next()
}

func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		logger.Info("http request", "method", c.Request.Method, "path", c.Request.URL.Path,
			"status", c.Writer.Status(), "duration_ms", time.Since(started).Milliseconds())
	}
}

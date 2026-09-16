package app

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/observability"
	transporthttp "github.com/nanashiwang/meta-pulse/internal/transport/http"
)

const readinessTimeout = 2 * time.Second

type ReadinessChecker interface {
	Check(context.Context) error
}

// APIRoutes carries the optional handlers a deployment wires in. A nil field
// simply leaves that route unregistered, so a partially configured process
// still serves health checks instead of failing to start.
type APIRoutes struct {
	Profile    transporthttp.ProfileReader
	Summary    transporthttp.SummaryReader
	Action     transporthttp.ActionExecutor
	Content    transporthttp.ContentAwardExecutor
	History    transporthttp.RewardHistoryReader
	Operations transporthttp.OperationsOverviewReader
	// Auth guards every route above. Without it none are registered: an
	// unauthenticated internal route would expose user data.
	Auth gin.HandlerFunc
}

func NewRouter(logger *slog.Logger, readiness ReadinessChecker, metrics ...*observability.Metrics) *gin.Engine {
	return NewRouterWithRoutes(logger, readiness, APIRoutes{}, metrics...)
}

func NewRouterWithRoutes(logger *slog.Logger, readiness ReadinessChecker, routes APIRoutes, metrics ...*observability.Metrics) *gin.Engine {
	if logger == nil {
		logger = slog.Default()
	}

	router := gin.New()
	router.Use(gin.Recovery(), requestLogger(logger))
	if len(metrics) > 0 && metrics[0] != nil {
		router.Use(requestMetrics(metrics[0]))
	}
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "meta-pulse-api"})
	})
	if len(metrics) > 0 && metrics[0] != nil {
		router.GET("/metrics", gin.WrapH(metrics[0].Handler()))
	}
	router.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), readinessTimeout)
		defer cancel()
		if readiness == nil || readiness.Check(ctx) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "service": "meta-pulse-api"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "meta-pulse-api"})
	})
	if routes.Auth == nil {
		return router
	}
	internal := router.Group("/v1/internal")
	if routes.Profile != nil {
		transporthttp.ProfileRoute(internal, routes.Profile, routes.Auth)
	}
	if routes.Summary != nil {
		transporthttp.SummaryRoute(internal, routes.Summary, routes.Auth)
	}
	if routes.Action != nil {
		transporthttp.ActionRoute(internal, routes.Action, routes.Auth)
	}
	if routes.Content != nil {
		transporthttp.ContentAwardRoute(internal, routes.Content, routes.Auth)
	}
	if routes.History != nil {
		transporthttp.RewardHistoryRoute(internal, routes.History, routes.Auth)
	}
	if routes.Operations != nil {
		transporthttp.OperationsOverviewRoute(internal, routes.Operations, routes.Auth)
	}
	return router
}

func requestMetrics(metrics *observability.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		method := c.Request.Method
		switch method {
		case "GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH":
		default:
			method = "OTHER"
		}
		metrics.HTTPRequests.WithLabelValues(method, path, strconv.Itoa(c.Writer.Status())).Inc()
	}
}

func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		logger.Info("http request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(started).Milliseconds(),
		)
	}
}

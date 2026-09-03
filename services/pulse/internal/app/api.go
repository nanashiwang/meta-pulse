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

func NewRouter(logger *slog.Logger, readiness ReadinessChecker, metrics ...*observability.Metrics) *gin.Engine {
	return NewRouterWithProfile(logger, readiness, nil, nil, metrics...)
}

func NewRouterWithProfile(logger *slog.Logger, readiness ReadinessChecker, profile transporthttp.ProfileReader, profileAuth gin.HandlerFunc, metrics ...*observability.Metrics) *gin.Engine {
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
	if profile != nil && profileAuth != nil {
		transporthttp.ProfileRoute(router.Group("/v1/internal"), profile, profileAuth)
	}
	return router
}

func requestMetrics(metrics *observability.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		metrics.HTTPRequests.WithLabelValues(c.Request.Method, c.Request.URL.Path, strconv.Itoa(c.Writer.Status())).Inc()
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

package transporthttp

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type PeriodAdministrator interface {
	ListForAdmin(context.Context) ([]service.AdminPeriodView, error)
	CreateFromAdmin(context.Context, service.PeriodAdminRequest, string, string) (service.PeriodCreateResult, error)
}

func PeriodAdminRoutes(router *gin.RouterGroup, svc PeriodAdministrator, auth gin.HandlerFunc) {
	if router == nil || svc == nil || auth == nil {
		return
	}
	guard := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := PrincipalWithRole(c, "admin"); !ok {
			c.AbortWithStatusJSON(403, gin.H{"error": "forbidden"})
			return
		}
		if c.Request.URL.RawQuery != "" {
			c.AbortWithStatusJSON(400, gin.H{"error": "invalid_request"})
			return
		}
		c.Next()
	}
	router.GET("/admin/periods", auth, guard, func(c *gin.Context) {
		periods, err := svc.ListForAdmin(c.Request.Context())
		if err != nil {
			periodAdminError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"periods": periods})
	})
	router.PUT("/admin/periods", auth, guard, func(c *gin.Context) {
		key := c.GetHeader("Idempotency-Key")
		if len(c.Request.Header.Values("Idempotency-Key")) != 1 || !settingsRequestID.MatchString(key) {
			c.JSON(400, gin.H{"error": "invalid_idempotency_key"})
			return
		}
		var request service.PeriodAdminRequest
		if err := bindSettingsJSON(c, &request); err != nil {
			c.JSON(400, gin.H{"error": "invalid_request"})
			return
		}
		principal, _ := Principal(c)
		result, err := svc.CreateFromAdmin(c.Request.Context(), request, strconv.FormatUint(principal.UserID, 10), key)
		if err != nil {
			periodAdminError(c, err)
			return
		}
		c.JSON(http.StatusOK, result)
	})
}
func periodAdminError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidPeriod):
		c.JSON(400, gin.H{"error": "invalid_period"})
	case errors.Is(err, ports.ErrConflict):
		c.JSON(409, gin.H{"error": "period_conflict"})
	default:
		c.JSON(503, gin.H{"error": "settings_unavailable"})
	}
}

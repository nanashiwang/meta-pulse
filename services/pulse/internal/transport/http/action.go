package transporthttp

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type ActionExecutor interface {
	Execute(context.Context, service.ActionCommand) (service.ActionResult, error)
}

// ActionRoute is the only mutating product route. Identity comes from the
// verified Principal and replay protection comes from the required header.
func ActionRoute(router *gin.RouterGroup, executor ActionExecutor, auth gin.HandlerFunc) {
	if router == nil || executor == nil || auth == nil {
		return
	}
	router.POST("/me/actions", auth, func(c *gin.Context) {
		principal, ok := Principal(c)
		if !ok || principal.UserID == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		if idempotencyKey == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key is required"})
			return
		}
		var request struct {
			ActionID    string `json:"action_id"`
			TriggerType string `json:"trigger_type"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid action payload"})
			return
		}
		result, err := executor.Execute(c.Request.Context(), service.ActionCommand{
			UserID: principal.UserID, ActionID: strings.TrimSpace(request.ActionID),
			TriggerType: strings.TrimSpace(request.TriggerType), IdempotencyKey: idempotencyKey,
		})
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, service.ErrMissingIdempotencyKey), errors.Is(err, service.ErrInvalidAction):
				status = http.StatusBadRequest
			case errors.Is(err, service.ErrInsufficientTickets), errors.Is(err, service.ErrBudgetExceeded), errors.Is(err, ledger.ErrIdempotencyConflict):
				status = http.StatusConflict
			}
			c.JSON(status, gin.H{"error": "action failed"})
			return
		}
		c.JSON(http.StatusCreated, result)
	})
}

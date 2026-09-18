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
		c.Header("Cache-Control", "no-store")
		principal, ok := ProductPrincipal(c)
		if !ok {
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
		if err := bindStrictJSON(c, &request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid action payload"})
			return
		}
		triggerType := strings.TrimSpace(request.TriggerType)
		if triggerType != service.ActionTriggerType {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid action payload"})
			return
		}
		result, err := executor.Execute(c.Request.Context(), service.ActionCommand{
			UserID: principal.UserID, ActionID: strings.TrimSpace(request.ActionID),
			TriggerType: triggerType, IdempotencyKey: idempotencyKey,
		})
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, service.ErrActionsUnavailable):
				status = http.StatusServiceUnavailable
			case errors.Is(err, service.ErrMissingIdempotencyKey), errors.Is(err, service.ErrInvalidAction):
				status = http.StatusBadRequest
			case errors.Is(err, service.ErrInsufficientTickets), errors.Is(err, service.ErrBudgetExceeded), errors.Is(err, ledger.ErrIdempotencyConflict):
				status = http.StatusConflict
			}
			code := "action_pending"
			switch {
			case errors.Is(err, service.ErrInsufficientTickets):
				code = "insufficient_tickets"
			case errors.Is(err, service.ErrBudgetExceeded):
				code = "budget_exceeded"
			case errors.Is(err, ledger.ErrIdempotencyConflict):
				code = "idempotency_conflict"
			case errors.Is(err, service.ErrActionsUnavailable):
				code = "actions_unavailable"
			case status == http.StatusBadRequest:
				code = "invalid_action"
			}
			c.JSON(status, gin.H{"error": code})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"grant_id": result.GrantID, "period_id": result.PeriodID, "action_id": result.ActionID, "reward_type": result.RewardType, "amount": result.Amount, "status": result.Status})
	})
}

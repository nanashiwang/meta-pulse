package transporthttp

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type ContentAwardExecutor interface {
	ReviewAndAward(context.Context, service.ContentAwardCommand) (service.ContentAwardResult, error)
	Reverse(context.Context, string, string, string, string, string) error
}

// ContentAwardRoute is restricted to the admin role. Candidate review and
// reversal never trust an actor id from JSON; it is derived from the signed
// admin principal.
func ContentAwardRoute(router *gin.RouterGroup, executor ContentAwardExecutor, auth gin.HandlerFunc) {
	if router == nil || executor == nil || auth == nil {
		return
	}
	admin := func(c *gin.Context) {
		principal, ok := Principal(c)
		if !ok || principal.UserID == 0 || principal.Role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
			return
		}
		c.Next()
	}
	router.POST("/admin/content-awards", auth, admin, func(c *gin.Context) {
		principal, _ := Principal(c)
		requestID := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		if requestID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key is required"})
			return
		}
		var request struct {
			CandidateID  uint64 `json:"candidate_id"`
			AwardVersion uint64 `json:"award_version"`
			PeriodID     uint64 `json:"period_id"`
			RewardType   string `json:"reward_type"`
			Amount       int64  `json:"amount"`
			Reason       string `json:"reason"`
		}
		if err := bindStrictJSON(c, &request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid content award payload"})
			return
		}
		result, err := executor.ReviewAndAward(c.Request.Context(), service.ContentAwardCommand{CandidateID: request.CandidateID, AwardVersion: request.AwardVersion, PeriodID: request.PeriodID, RewardType: strings.TrimSpace(request.RewardType), Amount: request.Amount, Reason: strings.TrimSpace(request.Reason), ActorType: "admin", ActorID: strconv.FormatUint(principal.UserID, 10), RequestID: requestID})
		if err != nil {
			contentAwardError(c, err)
			return
		}
		c.JSON(http.StatusCreated, result)
	})
	router.POST("/admin/content-awards/:action_id/reverse", auth, admin, func(c *gin.Context) {
		principal, _ := Principal(c)
		requestID := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		if requestID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key is required"})
			return
		}
		var request struct {
			Reason string `json:"reason"`
		}
		if err := bindStrictJSON(c, &request); err != nil || strings.TrimSpace(request.Reason) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "reason is required"})
			return
		}
		if err := executor.Reverse(c.Request.Context(), c.Param("action_id"), "admin", strconv.FormatUint(principal.UserID, 10), strings.TrimSpace(request.Reason), requestID); err != nil {
			contentAwardError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
}

func contentAwardError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, service.ErrContentCandidateUnavailable), errors.Is(err, service.ErrContentPaidThreshold), errors.Is(err, service.ErrContentAwardLimit), errors.Is(err, ledger.ErrIdempotencyConflict):
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"error": "content award failed"})
}

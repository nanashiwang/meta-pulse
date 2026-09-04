package transporthttp

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type SummaryReader interface {
	GetSummary(context.Context, uint64, time.Time) (service.PulseSummary, error)
}

// SummaryRoute derives the user from the authenticated principal. There is no
// user ID path/query parameter that a browser can use to select another user.
func SummaryRoute(router *gin.RouterGroup, reader SummaryReader, auth gin.HandlerFunc) {
	if router == nil || reader == nil || auth == nil {
		return
	}
	router.GET("/me/summary", auth, func(c *gin.Context) {
		principal, ok := PrincipalWithRole(c, "new-api")
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		summary, err := reader.GetSummary(c.Request.Context(), principal.UserID, time.Now())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "summary unavailable"})
			return
		}
		response := gin.H{
			"user_id":                     summary.Profile.UserID,
			"lifetime_contribution_milli": summary.Profile.LifetimeContribution,
			"level": gin.H{
				"key":  summary.Profile.Level.Key,
				"name": summary.Profile.Level.Name,
			},
			"current_contribution_milli": summary.CurrentContribution,
			"available_tickets":          summary.AvailableTickets,
			"ledger":                     ledgerResponse(summary.CurrentLedgerEntries),
		}
		if summary.CurrentPeriod != nil {
			response["current_period"] = gin.H{
				"id":             summary.CurrentPeriod.ID,
				"key":            summary.CurrentPeriod.Key,
				"status":         summary.CurrentPeriod.Status,
				"starts_at":      summary.CurrentPeriod.StartsAt,
				"ends_at":        summary.CurrentPeriod.EndsAt,
				"timezone":       summary.CurrentPeriod.Timezone,
				"config_version": summary.CurrentPeriod.ConfigVersion,
			}
		} else {
			response["current_period"] = nil
		}
		c.JSON(http.StatusOK, response)
	})
}

type ledgerResponseEntry struct {
	ID           uint64           `json:"id"`
	AssetType    ledger.AssetType `json:"asset_type"`
	Operation    ledger.Operation `json:"operation"`
	Amount       int64            `json:"amount"`
	BalanceAfter int64            `json:"balance_after"`
	SourceType   string           `json:"source_type"`
	SourceRef    string           `json:"source_ref"`
	CreatedAt    time.Time        `json:"created_at"`
}

func ledgerResponse(entries []ledger.Entry) []ledgerResponseEntry {
	result := make([]ledgerResponseEntry, len(entries))
	for i, entry := range entries {
		result[i] = ledgerResponseEntry{
			ID: entry.ID, AssetType: entry.AssetType, Operation: entry.Operation,
			Amount: entry.Amount, BalanceAfter: entry.BalanceAfter,
			SourceType: entry.SourceType, SourceRef: entry.SourceRef, CreatedAt: entry.CreatedAt,
		}
	}
	return result
}

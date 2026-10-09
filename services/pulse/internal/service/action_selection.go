package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

var (
	ErrSelectionRequired = errors.New("selection confirmation required")
	ErrSelectionChanged  = errors.New("ticket selection changed")
)

// No amount, prize, budget or trusted browser identity is accepted. The MAC
// binds the opaque snapshot to the authenticated principal; amounts stay in DB.
type ticketSelection struct {
	PeriodID       uint64 `json:"period_id"`
	SourcePeriodID uint64 `json:"source_period_id"`
	ConfigVersion  string `json:"config_version"`
	LotID          uint64 `json:"lot_id"`
	ExperienceOnly bool   `json:"experience_only"`
}

func selectionMAC(secret []byte, userID uint64, payload string) []byte {
	h := hmac.New(sha256.New, secret)
	fmt.Fprintf(h, "pulse-ticket-selection:v3:%d:%s", userID, payload)
	return h.Sum(nil)
}
func signSelection(secret []byte, userID uint64, p period.Period, lot *ports.TicketLot, experienceOnly bool, sourcePeriodID ...uint64) string {
	if len(secret) == 0 {
		return ""
	}
	choice := ticketSelection{PeriodID: p.ID, SourcePeriodID: p.ID, ConfigVersion: p.ConfigVersion, ExperienceOnly: experienceOnly}
	if lot != nil {
		choice.LotID = lot.ID
		choice.SourcePeriodID = lot.PeriodID
	}
	if len(sourcePeriodID) > 0 {
		choice.SourcePeriodID = sourcePeriodID[0]
	}
	raw, _ := json.Marshal(choice)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + base64.RawURLEncoding.EncodeToString(selectionMAC(secret, userID, payload))
}
func verifySelection(secret []byte, userID uint64, token string) (ticketSelection, error) {
	var choice ticketSelection
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(token) > 1024 {
		return choice, ErrSelectionChanged
	}
	mac, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(mac, selectionMAC(secret, userID, parts[0])) {
		return choice, ErrSelectionChanged
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(raw, &choice) != nil || choice.PeriodID == 0 || choice.SourcePeriodID == 0 || choice.ConfigVersion == "" {
		return choice, ErrSelectionChanged
	}
	return choice, nil
}

// Serialize new rule publication and new draws, without rewriting frozen rules.
func lockRewardRule(ctx context.Context, repo ports.IdempotencyRepository) error {
	_, err := repo.GetOrCreateForUpdate(ctx, "period_create_lock", "global", "447cc9dbdc73a33ea5be9cef405e81ef56c4e23b9202238aee120a0078c2fb2a")
	return err
}
func selectedTicket(ctx context.Context, repos ports.Repositories, choice ticketSelection, userID uint64, now time.Time) (period.Period, period.Period, *ports.TicketLot, error) {
	var current, source period.Period
	fail := func(err error) (period.Period, period.Period, *ports.TicketLot, error) {
		return current, source, nil, err
	}
	if repos.Tickets == nil {
		return fail(errors.New("ticket repository unavailable"))
	}
	var err error
	// This is a current read: earlier legacy-recovery queries may already have
	// established a repeatable-read snapshot before the publication mutex.
	current, err = repos.Period.FindActiveAtCurrent(ctx, now)
	if errors.Is(err, period.ErrNoActivePeriod) {
		return fail(ErrSelectionChanged)
	}
	if err != nil {
		return fail(err)
	}
	if current.ID != choice.PeriodID || current.ConfigVersion != choice.ConfigVersion {
		return fail(ErrSelectionChanged)
	}
	if err = repos.Tickets.LockUser(ctx, userID); err != nil {
		return fail(err)
	}
	source, err = repos.Tickets.Period(ctx, choice.SourcePeriodID)
	if err != nil {
		return fail(err)
	}
	if !source.Continuous {
		if choice.LotID != 0 || choice.ExperienceOnly || source.Status != period.StatusActive || !source.Contains(now) {
			return fail(ErrSelectionChanged)
		}
		return current, source, nil, nil
	}
	lot, err := repos.Tickets.LotForUpdate(ctx, choice.LotID)
	if err != nil {
		return fail(err)
	}
	if lot == nil || lot.UserID != userID || lot.PeriodID != source.ID || lot.Remaining <= 0 || (!now.Before(lot.QuotaExpiresAt)) != choice.ExperienceOnly {
		return fail(ErrSelectionChanged)
	}
	return current, source, lot, nil
}

// Store only final business refusals before ANY financial write. Returning nil
// from the UoW commits both identities; errors are returned outside the UoW.
// Infrastructure errors and commit failures never become a no-charge receipt.
func actionRefusalCode(err error) string {
	switch {
	case errors.Is(err, ErrSelectionRequired):
		return "selection_required"
	case errors.Is(err, ErrSelectionChanged):
		return "selection_changed"
	case errors.Is(err, ErrActionsUnavailable):
		return "actions_unavailable"
	case errors.Is(err, ErrInsufficientTickets):
		return "insufficient_tickets"
	case errors.Is(err, ErrBudgetExceeded):
		return "budget_exceeded"
	}
	return ""
}
func actionRefusalError(code string) error {
	for _, err := range []error{ErrSelectionRequired, ErrSelectionChanged, ErrActionsUnavailable, ErrInsufficientTickets, ErrBudgetExceeded} {
		if actionRefusalCode(err) == code {
			return err
		}
	}
	return errors.New("invalid action refusal receipt")
}
func saveActionRefusal(ctx context.Context, repo ports.IdempotencyRepository, record ports.IdempotencyRecord, code string) error {
	status := 409
	record.ResponseStatus = &status
	record.ResponseJSON, _ = json.Marshal(struct {
		Error string `json:"error"`
	}{code})
	record.ResourceType = "action_refusal"
	record.ResourceID = record.Key
	return repo.Save(ctx, record)
}
func replayActionResponse(record ports.IdempotencyRecord, result *ActionResult, refusal *error) error {
	if record.ResourceType == "action_refusal" {
		var response struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(record.ResponseJSON, &response); err != nil {
			return err
		}
		*refusal = actionRefusalError(response.Error)
		return nil
	}
	return json.Unmarshal(record.ResponseJSON, result)
}

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
	ConfigVersion  string `json:"config_version"`
	LotID          uint64 `json:"lot_id"`
	ExperienceOnly bool   `json:"experience_only"`
}

func selectionMAC(secret []byte, userID uint64, payload string) []byte {
	h := hmac.New(sha256.New, secret)
	fmt.Fprintf(h, "pulse-ticket-selection:v2:%d:%s", userID, payload)
	return h.Sum(nil)
}
func signSelection(secret []byte, userID uint64, p period.Period, lot *ports.TicketLot, experienceOnly bool) string {
	if len(secret) == 0 {
		return ""
	}
	choice := ticketSelection{PeriodID: p.ID, ConfigVersion: p.ConfigVersion, ExperienceOnly: experienceOnly}
	if lot != nil {
		choice.LotID = lot.ID
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
	if err != nil || json.Unmarshal(raw, &choice) != nil || choice.PeriodID == 0 || choice.ConfigVersion == "" {
		return choice, ErrSelectionChanged
	}
	return choice, nil
}
func selectedTicket(ctx context.Context, repos ports.Repositories, choice ticketSelection, userID uint64, now time.Time) (period.Period, *ports.TicketLot, error) {
	if repos.Tickets == nil {
		return period.Period{}, nil, errors.New("ticket repository unavailable")
	}
	if err := repos.Tickets.LockUser(ctx, userID); err != nil {
		return period.Period{}, nil, err
	}
	p, err := repos.Tickets.Period(ctx, choice.PeriodID)
	if err != nil {
		return p, nil, err
	}
	if p.ConfigVersion != choice.ConfigVersion || p.Status != period.StatusActive || !p.Contains(now) {
		return p, nil, ErrSelectionChanged
	}
	if !p.Continuous {
		if choice.LotID != 0 || choice.ExperienceOnly {
			return p, nil, ErrSelectionChanged
		}
		return p, nil, nil
	}
	lot, err := repos.Tickets.NextInGroup(ctx, userID, p.ID, choice.ExperienceOnly, now)
	if err != nil {
		return p, nil, err
	}
	if lot == nil || lot.ID != choice.LotID {
		return p, nil, ErrSelectionChanged
	}
	return p, lot, nil
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

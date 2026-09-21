package pulse_user_center

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var communityRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)

type communityPeriod struct {
	Continuous bool `json:"continuous"`

	ID            uint64    `json:"id"`
	Key           string    `json:"key"`
	Status        string    `json:"status,omitempty"`
	StartsAt      time.Time `json:"starts_at"`
	EndsAt        time.Time `json:"ends_at"`
	Timezone      string    `json:"timezone,omitempty"`
	ConfigVersion string    `json:"config_version"`
}

type communitySummary struct {
	LifetimeContribution int64            `json:"lifetime_contribution_milli"`
	CurrentContribution  int64            `json:"current_contribution_milli"`
	AvailableTickets     int64            `json:"available_tickets"`
	Level                ProfileLevel     `json:"level"`
	CurrentPeriod        *communityPeriod `json:"current_period"`
}

type communityReward struct {
	GrantID    string `json:"grant_id"`
	PeriodID   uint64 `json:"period_id"`
	ActionID   string `json:"action_id"`
	RewardType string `json:"reward_type"`
	Amount     int64  `json:"amount"`
	Status     string `json:"status"`
}

type communityRewardHistoryItem struct {
	communityReward
	CreatedAt time.Time `json:"created_at"`
}

type communityRules struct {
	QuotaValidityDays int        `json:"quota_validity_days"`
	QuotaExpiresAt    *time.Time `json:"quota_expires_at,omitempty"`
	ExperienceOnly    bool       `json:"experience_only"`

	Enabled           bool             `json:"enabled"`
	UnavailableReason string           `json:"unavailable_reason"`
	Period            *communityPeriod `json:"period"`
	TicketCost        int64            `json:"ticket_cost"`
	QuotaPerUnit      int64            `json:"quota_per_unit"`
	Rewards           []struct {
		Name       string `json:"name"`
		RewardType string `json:"reward_type"`
		Amount     int64  `json:"amount"`
		Weight     uint64 `json:"weight"`
	} `json:"rewards"`
	TotalWeight uint64 `json:"total_weight"`
}

// The router group is supplied only by Answer's MustAuthAndAccountAvailable
// plugin hook. Nginx exposes a fixed /metar/api/pulse alias to these handlers.
func (uc *UserCenter) registerCommunityRoutes(r *gin.RouterGroup, session func(*gin.Context) (string, error), siteURL func() string) {
	if r == nil {
		return
	}
	for _, operation := range []string{"summary", "rewards", "rules", "actions"} {
		method := http.MethodGet
		if operation == "actions" {
			method = http.MethodPost
		}
		r.Handle(method, "/metar/pulse/"+operation, uc.communityHandler(operation, session, siteURL))
	}
}

func (uc *UserCenter) communityHandler(operation string, session func(*gin.Context) (string, error), siteURL func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		// Answer supports query-string Authorization; product routes explicitly
		// reject it so session tokens cannot enter URLs or access logs.
		query, err := url.ParseQuery(c.Request.URL.RawQuery)
		if err != nil || !communityQueryAllowed(operation, query) {
			communityError(c, http.StatusBadRequest, "invalid_request")
			return
		}
		if c.GetHeader("Authorization") == "" {
			communityError(c, http.StatusUnauthorized, "authentication_required")
			return
		}
		forumID, err := session(c)
		if err != nil {
			communityIdentityError(c, err)
			return
		}
		var body []byte
		var idempotencyKey string
		if operation == "actions" {
			if !communitySameOrigin(c.Request, siteURL()) {
				communityError(c, http.StatusForbidden, "origin_rejected")
				return
			}
			contentType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
			if err != nil || contentType != "application/json" {
				communityError(c, http.StatusBadRequest, "invalid_request")
				return
			}
			idempotencyKey = c.GetHeader("Idempotency-Key")
			if !communityRequestID.MatchString(idempotencyKey) || len(c.Request.Header.Values("Idempotency-Key")) != 1 {
				communityError(c, http.StatusBadRequest, "invalid_idempotency_key")
				return
			}
			data, err := io.ReadAll(io.LimitReader(c.Request.Body, 4097))
			var payload struct {
				ActionID string `json:"action_id"`
			}
			if err != nil || len(data) > 4096 || !singleActionJSON(data, &payload.ActionID) || !communityRequestID.MatchString(payload.ActionID) {
				communityError(c, http.StatusBadRequest, "invalid_request")
				return
			}
			body, _ = json.Marshal(struct {
				ActionID    string `json:"action_id"`
				TriggerType string `json:"trigger_type"`
			}{payload.ActionID, "pulse"})
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		// Hold the read lock until the request completes so an admin config
		// refresh cannot close the guard DB while the binding is being checked.
		uc.runtimeMu.RLock()
		defer uc.runtimeMu.RUnlock()
		reader, ok := uc.Guard.(communityIdentityReader)
		if !ok || uc.Client == nil || uc.Config == nil || !usableConfigSecret(uc.Config.CommunityBFFHMACSecret) {
			communityError(c, http.StatusServiceUnavailable, "pulse_unavailable")
			return
		}
		externalID, err := reader.CommunityIdentity(ctx, forumID)
		if err != nil {
			communityIdentityError(c, err)
			return
		}
		path := "/v1/internal/me/" + operation
		if encoded := query.Encode(); encoded != "" {
			path += "?" + encoded
		}
		data, status, err := uc.Client.communityRequest(ctx, c.Request.Method, path, externalID, idempotencyKey, body)
		if err != nil || status >= 500 {
			code := "pulse_unavailable"
			if operation == "actions" {
				code = "action_pending"
			}
			communityError(c, http.StatusServiceUnavailable, code)
			return
		}
		if status != http.StatusOK && status != http.StatusCreated {
			if operation == "actions" && (status == http.StatusBadRequest || status == http.StatusConflict) {
				var failure struct {
					Error string `json:"error"`
				}
				if decodeCommunityResponse(data, &failure) == nil {
					switch failure.Error {
					case "insufficient_tickets", "budget_exceeded", "invalid_action":
						communityError(c, status, "action_rejected")
						return
					}
				}
			}
			switch status {
			case http.StatusConflict:
				communityError(c, status, "action_conflict")
			case http.StatusTooManyRequests:
				communityError(c, status, "rate_limited")
			case http.StatusBadRequest:
				communityError(c, status, "invalid_request")
			default:
				communityError(c, http.StatusServiceUnavailable, "pulse_unavailable")
			}
			return
		}
		projection, err := communityProjection(operation, externalID, data)
		if err != nil {
			code := "pulse_unavailable"
			if operation == "actions" {
				code = "action_pending"
			}
			communityError(c, http.StatusServiceUnavailable, code)
			return
		}

		// The original result remains authoritative even if optional EXP
		// delivery is delayed. Refresh drains the durable community outbox.
		if operation == "actions" || operation == "rewards" {
			_ = uc.consumePulseExperience(ctx, forumID, externalID)
		}
		c.JSON(status, projection)
	}
}

func communityQueryAllowed(operation string, query url.Values) bool {
	for key, values := range query {
		if operation != "rewards" || len(values) != 1 {
			return false
		}
		switch key {
		case "action_id":
			if !communityRequestID.MatchString(values[0]) {
				return false
			}
		case "limit":
			n, err := strconv.Atoi(values[0])
			if err != nil || n < 1 || n > 100 || strconv.Itoa(n) != values[0] {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func communitySameOrigin(r *http.Request, configuredSiteURL string) bool {
	if r.Header.Get("X-Metar-Request") != "1" || len(r.Header.Values("Origin")) != 1 {
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return false
	}
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	site, err := url.Parse(configuredSiteURL)
	return err == nil && site.User == nil && (site.Scheme == "https" || site.Scheme == "http") && site.Host != "" && origin.Scheme == site.Scheme && strings.EqualFold(origin.Host, site.Host)
}

// Decode tokens explicitly: DisallowUnknownFields alone permits duplicate
// action_id keys, which creates ambiguity between browser, proxy and service.
func singleActionJSON(data []byte, actionID *string) bool {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') || !d.More() {
		return false
	}
	key, err := d.Token()
	if err != nil || key != "action_id" || d.Decode(actionID) != nil || d.More() {
		return false
	}
	token, err = d.Token()
	if err != nil || token != json.Delim('}') {
		return false
	}
	var trailing any
	return errors.Is(d.Decode(&trailing), io.EOF)
}

func decodeCommunityResponse(data []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(target); err != nil {
		return err
	}
	var trailing any
	if !errors.Is(d.Decode(&trailing), io.EOF) {
		return errors.New("invalid trailing response")
	}
	return nil
}

func communityProjection(operation, externalID string, data []byte) (any, error) {
	switch operation {
	case "summary":
		var raw struct {
			communitySummary
			UserID uint64 `json:"user_id"`
		}
		if err := decodeCommunityResponse(data, &raw); err != nil {
			return nil, err
		}
		if strconv.FormatUint(raw.UserID, 10) != externalID {
			return nil, errors.New("summary identity mismatch")
		}
		return raw.communitySummary, nil
	case "rewards":
		var result struct {
			Rewards []communityRewardHistoryItem `json:"rewards"`
		}
		if err := decodeCommunityResponse(data, &result); err != nil {
			return nil, err
		}
		if result.Rewards == nil {
			result.Rewards = []communityRewardHistoryItem{}
		}
		return result, nil
	case "rules":
		var result communityRules
		if err := decodeCommunityResponse(data, &result); err != nil {
			return nil, err
		}
		return result, nil
	case "actions":
		var result communityReward
		if err := decodeCommunityResponse(data, &result); err != nil {
			return nil, err
		}
		if result.GrantID == "" || result.ActionID == "" || result.PeriodID == 0 {
			return nil, errors.New("invalid action response")
		}
		return result, nil
	}
	return nil, errors.New("unsupported community operation")
}

func communityError(c *gin.Context, status int, code string) {
	c.AbortWithStatusJSON(status, gin.H{"error": code})
}

func communityIdentityError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errCommunityUnauthenticated):
		communityError(c, http.StatusUnauthorized, errCommunityUnauthenticated.Error())
	case errors.Is(err, errCommunityAccountUnavailable):
		communityError(c, http.StatusForbidden, errCommunityAccountUnavailable.Error())
	case errors.Is(err, errCommunityBindingRequired):
		communityError(c, http.StatusForbidden, errCommunityBindingRequired.Error())
	default:
		communityError(c, http.StatusServiceUnavailable, "pulse_unavailable")
	}
}

package pulse_user_center

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

func validAdminPeriodBody(fields map[string]json.RawMessage) bool {
	if len(fields) != 7 && len(fields) != 8 {
		return false
	}
	for _, name := range []string{"key", "starts_at", "reason", "multiplier_bps", "ticket_threshold_milli", "reward_budget", "rewards"} {
		if _, ok := fields[name]; !ok {
			return false
		}
	}
	for name, raw := range fields {
		switch name {
		case "key", "starts_at", "reason":
			var value string
			if json.Unmarshal(raw, &value) != nil || value == "" {
				return false
			}
		case "multiplier_bps", "ticket_threshold_milli", "reward_budget", "experience_budget":
			var value int64
			if json.Unmarshal(raw, &value) != nil || value < 0 || ((name == "multiplier_bps" || name == "ticket_threshold_milli") && value == 0) || value > 1<<53-1 {
				return false
			}
		case "rewards":
			var rewards []json.RawMessage
			if json.Unmarshal(raw, &rewards) != nil || len(rewards) < 1 || len(rewards) > 50 {
				return false
			}
			for _, reward := range rewards {
				fields, ok := adminJSONObject(reward)
				if !ok || (len(fields) != 3 && len(fields) != 4) {
					return false
				}

				for name := range fields {
					if name != "key" && name != "amount" && name != "weight" && name != "reward_type" {
						return false
					}
				}
				if raw, ok := fields["reward_type"]; ok {
					var kind string
					if json.Unmarshal(raw, &kind) != nil || (kind != "newapi_quota" && kind != "community_exp") {
						return false
					}
				}
				var key string
				var amount, weight int64
				if json.Unmarshal(fields["key"], &key) != nil || key == "" || json.Unmarshal(fields["amount"], &amount) != nil || amount <= 0 || json.Unmarshal(fields["weight"], &weight) != nil || weight <= 0 {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

type adminPeriodResult struct {
	PeriodID             uint64    `json:"period_id"`
	Key                  string    `json:"period_key"`
	Status               string    `json:"status"`
	StartsAt             time.Time `json:"starts_at"`
	EndsAt               time.Time `json:"ends_at"`
	TicketThresholdMilli int64     `json:"ticket_threshold_milli"`
}
type adminPeriod struct {
	ID                   uint64    `json:"id"`
	Key                  string    `json:"key"`
	Status               string    `json:"status"`
	StartsAt             time.Time `json:"starts_at"`
	EndsAt               time.Time `json:"ends_at"`
	TicketThresholdMilli int64     `json:"ticket_threshold_milli"`
	Rules                []struct {
		Key           string `json:"key"`
		MultiplierBps int32  `json:"multiplier_bps"`
	} `json:"rules"`
}

func adminPeriodsProjection(method string, data []byte) (any, error) {
	invalid := errors.New("invalid period response")
	if method == http.MethodPut {
		var result adminPeriodResult
		if decodeCommunityResponse(data, &result) != nil || result.PeriodID == 0 || result.Status != "active" || result.TicketThresholdMilli <= 0 || !result.EndsAt.After(result.StartsAt) {
			return nil, invalid
		}
		return result, nil
	}
	var result struct {
		Periods []adminPeriod `json:"periods"`
	}
	if decodeCommunityResponse(data, &result) != nil || result.Periods == nil || len(result.Periods) > 20 {
		return nil, invalid
	}
	return result, nil
}

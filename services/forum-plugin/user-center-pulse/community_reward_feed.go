package pulse_user_center

import (
	"encoding/json"
	"errors"
	"time"
)

// A dedicated allowlist projection: even an overbroad upstream response cannot
// expose identities or grant identifiers through the community feed.
type communityRewardFeedItem struct {
	RewardType string    `json:"reward_type"`
	Amount     int64     `json:"amount"`
	WonAt      time.Time `json:"won_at"`
}

type communityRewardFeed struct {
	Rewards      []communityRewardFeedItem `json:"rewards"`
	QuotaPerUnit int64                     `json:"quota_per_unit"`
}

func communityRewardFeedProjection(data []byte) (communityRewardFeed, error) {
	var result communityRewardFeed
	fields, ok := adminJSONObject(data)
	if !ok || string(fields["quota_per_unit"]) == "null" || json.Unmarshal(fields["quota_per_unit"], &result.QuotaPerUnit) != nil || json.Unmarshal(fields["rewards"], &result.Rewards) != nil || result.Rewards == nil || len(result.Rewards) > 20 || result.QuotaPerUnit < 0 || result.QuotaPerUnit > (1<<53)-1 {
		return communityRewardFeed{}, errors.New("invalid community reward feed")
	}
	for i := range result.Rewards {
		row := &result.Rewards[i]
		if (row.RewardType != "newapi_quota" && row.RewardType != "community_exp") || row.Amount <= 0 || row.Amount > (1<<53)-1 || row.WonAt.IsZero() {
			return communityRewardFeed{}, errors.New("invalid community reward item")
		}
		row.WonAt = row.WonAt.UTC().Truncate(time.Minute)
	}
	return result, nil
}

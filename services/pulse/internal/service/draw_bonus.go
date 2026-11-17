package service

import (
	"crypto/sha256"
	"strings"

	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
)

const drawBonusVersionPrefix = "draw-bonus-v1-"
const DrawBonusPolicyKey = drawBonusVersionPrefix + "20261118"
const drawBonusPercentage = 10

func usesDrawBonus(version string) bool { return strings.HasPrefix(version, drawBonusVersionPrefix) }

func quotaOnly(defs []reward.Definition) []reward.Definition {
	result := make([]reward.Definition, 0, len(defs))
	for _, d := range defs {
		if d.RewardType == "newapi_quota" {
			result = append(result, d)
		}
	}
	return result
}

// The same immutable HMAC seed determines both parts of one outcome. Domain
// separation keeps the bonus independent of the selected flash tier.
func selectDrawPrizes(defs []reward.Definition, seed [32]byte, bonusMode, expired bool) (reward.Definition, int64, error) {
	if expired {
		d, err := reward.SelectWeighted(experienceOnly(defs), seed)
		return d, 0, err
	}
	if !bonusMode {
		d, err := reward.SelectWeighted(defs, seed)
		return d, 0, err
	}
	primary, err := reward.SelectWeighted(quotaOnly(defs), seed)
	if err != nil {
		return primary, 0, err
	}
	bonus, err := selectBonusExperience(experienceOnly(defs), seed)
	return primary, bonus, err
}

func selectBonusExperience(exp []reward.Definition, seed [32]byte) (int64, error) {
	var sum uint64
	for _, d := range exp {
		if d.Enabled {
			if d.Weight > maxPublicRewardInteger-sum {
				return 0, reward.ErrWeightOverflow
			}
			sum += d.Weight
		}
	}
	if sum == 0 {
		return 0, reward.ErrNoDefinition
	}
	// The zero-EXP outcome has nine times the entire EXP pool weight.
	// The selected EXP tier retains its relative weight within that pool.
	choices := append([]reward.Definition{{ID: 1, RewardKey: "no-bonus", RewardType: ExperienceRewardType, Weight: 9 * sum, ConfigVersion: "draw-bonus-v1", Enabled: true}}, exp...)
	derived := sha256.Sum256(append([]byte("meta-pulse/draw-bonus-v1\x00"), seed[:]...))
	d, err := reward.SelectWeighted(choices, derived)
	return d.Amount, err
}

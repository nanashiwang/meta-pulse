// Package economics evaluates immutable, versioned contribution rules.
package economics

import (
	"errors"
	"path"
	"sort"
	"strings"

	"github.com/nanashiwang/meta-pulse/internal/domain/money"
)

type Rule struct {
	ID            uint64
	Key           string
	Priority      int
	ModelPattern  string
	ChannelID     *uint64
	Eligible      bool
	MultiplierBps money.Bps
	ConfigVersion string
}

type Decision struct {
	RuleID        uint64
	RuleKey       string
	Eligible      bool
	MultiplierBps money.Bps
	ConfigVersion string
	Contribution  money.Milli
}

func ValidateRules(rules []Rule, configVersion string) error {
	if strings.TrimSpace(configVersion) == "" {
		return errors.New("economics config version is empty")
	}
	for _, rule := range rules {
		if rule.ID == 0 || strings.TrimSpace(rule.Key) == "" || rule.ConfigVersion != configVersion {
			return errors.New("economics rule config version mismatch")
		}
		if err := rule.MultiplierBps.Validate(); err != nil {
			return err
		}
		if rule.ModelPattern != "" {
			if _, err := path.Match(rule.ModelPattern, ""); err != nil {
				return errors.New("economics rule model pattern is invalid")
			}
		}
	}
	return nil
}

func (r Rule) Matches(modelName string, channelID uint64) bool {
	if r.ChannelID != nil && *r.ChannelID != channelID {
		return false
	}
	if strings.TrimSpace(r.ModelPattern) == "" {
		return true
	}
	matched, err := path.Match(r.ModelPattern, modelName)
	return err == nil && matched
}

// Select chooses highest priority first. For equal priority, the more
// specific rule (model + channel) wins, then the stable rule key breaks ties.
func Select(rules []Rule, modelName string, channelID uint64) (Rule, bool) {
	matched := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		if rule.Matches(modelName, channelID) {
			matched = append(matched, rule)
		}
	}
	if len(matched) == 0 {
		return Rule{}, false
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Priority != matched[j].Priority {
			return matched[i].Priority > matched[j].Priority
		}
		specificity := func(r Rule) int {
			score := 0
			if r.ChannelID != nil {
				score += 2
			}
			if r.ModelPattern != "" {
				score++
			}
			return score
		}
		if specificity(matched[i]) != specificity(matched[j]) {
			return specificity(matched[i]) > specificity(matched[j])
		}
		return matched[i].Key < matched[j].Key
	})
	return matched[0], true
}

func Evaluate(quotaDelta int64, rule Rule) (Decision, error) {
	if err := rule.MultiplierBps.Validate(); err != nil {
		return Decision{}, err
	}
	decision := Decision{RuleID: rule.ID, RuleKey: rule.Key, Eligible: rule.Eligible, MultiplierBps: rule.MultiplierBps, ConfigVersion: rule.ConfigVersion}
	if !rule.Eligible {
		return decision, nil
	}
	contribution, err := money.Milli(quotaDelta).MultiplyBps(rule.MultiplierBps)
	if err != nil {
		return Decision{}, err
	}
	decision.Contribution = contribution
	return decision, nil
}

package service

import (
	"testing"
	"time"
)

func TestContinuousRuleValidation(t *testing.T) {
	base := validCreateCommand()
	base.Continuous, base.Activate = true, true
	base.StartsAt = time.Time{}
	base.RequestID = "continuous-validation"
	base.QuotaValidityDays = 30
	base.TicketThresholdMilli = 1000
	base.ExperienceBudget = 100
	base.Rewards = []PeriodRewardSpec{{Key: "exp", RewardType: ExperienceRewardType, Amount: 10, Weight: 1}}
	for _, days := range []int{1, 30, 3650} {
		cmd := base
		cmd.QuotaValidityDays = days
		if _, err := normalizePeriodCreateCommand(cmd); err != nil {
			t.Fatalf("days %d: %v", days, err)
		}
	}
	for name, change := range map[string]func(*PeriodCreateCommand){
		"zero days":     func(c *PeriodCreateCommand) { c.QuotaValidityDays = 0 },
		"too many days": func(c *PeriodCreateCommand) { c.QuotaValidityDays = 3651 },
		"manual start":  func(c *PeriodCreateCommand) { c.StartsAt = time.Now() },
		"no experience": func(c *PeriodCreateCommand) { c.Rewards = nil },
		"draft":         func(c *PeriodCreateCommand) { c.Activate = false },
		"no replay key": func(c *PeriodCreateCommand) { c.RequestID = "" },
		"legacy age":    func(c *PeriodCreateCommand) { c.Continuous = false; c.StartsAt = time.Now() },
	} {
		t.Run(name, func(t *testing.T) {
			cmd := base
			change(&cmd)
			if _, err := normalizePeriodCreateCommand(cmd); err == nil {
				t.Fatal("invalid rule accepted")
			}
		})
	}
}

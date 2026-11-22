package events

import (
	"errors"
	"testing"
)

func TestGiftRulesChooseHighestEligibleOnly(t *testing.T) {
	cutoff := int64(3)
	a := GiftRule{Likes: &cutoff, UsageRaw: "100", Exp: &GiftRange{50, 50}}
	b := GiftRule{Comments: &cutoff, Tickets: &GiftRange{2, 2}}
	rules := []GiftRule{a, b}
	count := func(v int64) func() (int64, error) { return func() (int64, error) { return v, nil } }
	for _, tc := range []struct {
		name            string
		likes, comments int64
		raw             *int64
		want            *GiftRule
	}{{"both", 5, 5, giftNumber(101), &a}, {"exact threshold", 5, 5, giftNumber(100), &b}, {"missing usage", 5, 5, nil, &b}, {"only first", 5, 1, giftNumber(101), &a}, {"neither", 3, 3, giftNumber(101), nil}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := firstGiftMatch(rules, count(tc.likes), count(tc.comments), func() (*int64, error) { return tc.raw, nil })
			if err != nil || (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
	unavailable := errors.New("unavailable")
	calls := 0
	spend := func() (*int64, error) { calls++; return nil, unavailable }
	if got, err := firstGiftMatch(rules, count(5), count(5), spend); got != nil || !errors.Is(err, unavailable) {
		t.Fatalf("should not demote a potential match: %+v %v", got, err)
	}
	if got, err := firstGiftMatch(rules, count(3), count(5), spend); got == nil || *got != b || err != nil || calls != 1 {
		t.Fatalf("ineligible first rule should not need usage: %+v %v (calls=%d)", got, err, calls)
	}
}

func giftNumber(v int64) *int64 { return &v }

func TestGiftRulesValidateLegacyAndGroups(t *testing.T) {
	legacy := GiftRules{Name: "legacy", Minutes: 5, Tickets: &GiftRange{1, 1}}
	modern := GiftRules{Name: "modern", Minutes: 5, Groups: []GiftRule{{Tickets: &GiftRange{1, 1}}, {Exp: &GiftRange{2, 2}}}}
	if !legacy.valid() || !modern.valid() || len(legacy.orderedRules()) != 1 {
		t.Fatal("valid formats rejected")
	}
	modern.Flash = &GiftRange{1, 1}
	if modern.valid() {
		t.Fatal("mixed formats accepted")
	}
	modern.Flash = nil
	modern.Groups[1].Exp = &GiftRange{3, 2}
	if modern.valid() {
		t.Fatal("invalid lower-priority rule accepted")
	}
	modern.Groups = make([]GiftRule, maxGiftRules+1)
	if modern.valid() {
		t.Fatal("too many groups accepted")
	}
}

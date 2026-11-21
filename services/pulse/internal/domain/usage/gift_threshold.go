package usage

import (
	"errors"
	"strconv"
	"strings"
)

// GiftUsageThreshold returns the exact threshold in the API's integer units.
// One wan is 10,000 raw units; one yi is 10,000 wan.
func GiftUsageThreshold(amount, unit string) (int64, error) {
	digits := 0
	switch unit {
	case "wan":
		digits = 4
	case "yi":
		digits = 8
	default:
		return 0, errGiftUsageThreshold
	}
	whole, frac, decimal := strings.Cut(amount, ".")
	if len(amount) > 32 || whole == "" || decimal && frac == "" || len(frac) > digits {
		return 0, errGiftUsageThreshold
	}
	for _, c := range whole + frac {
		if c < '0' || c > '9' {
			return 0, errGiftUsageThreshold
		}
	}
	raw, e := strconv.ParseInt(whole+frac+strings.Repeat("0", digits-len(frac)), 10, 64)
	if e != nil {
		return 0, errGiftUsageThreshold
	}
	return raw, nil
}

var errGiftUsageThreshold = errors.New("invalid gift usage threshold")

func ExceedsGiftThreshold(recorded *int64, raw int64) bool {
	return raw >= 0 && recorded != nil && *recorded > raw
}

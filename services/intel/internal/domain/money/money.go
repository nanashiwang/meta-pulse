// Package money holds the fixed-point quantities Intel stores and compares.
// Floating point never touches prices, scores or speeds: inputs are parsed
// from decimal strings straight into scaled integers.
package money

import (
	"errors"
	"math"
	"strings"
)

// Scale factors for the stored units.
const (
	// MicroScale stores prices as micro-USD (1 USD = 1,000,000).
	MicroScale = 6
	// BpsScale stores percentages as basis points (12.34% = 1234).
	BpsScale = 2
	// MilliScale stores rates such as tokens/second as thousandths.
	MilliScale = 3
)

var (
	ErrInvalidDecimal = errors.New("invalid decimal")
	ErrTooPrecise     = errors.New("decimal has more fractional digits than the unit allows")
	ErrOverflow       = errors.New("decimal overflows int64")
)

// ParseScaled parses a non-negative decimal string such as "2.50" into an
// integer scaled by 10^scale. It rejects signs, exponents, separators and
// fractional digits beyond scale rather than rounding them away.
func ParseScaled(s string, scale int) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || scale < 0 || scale > 9 {
		return 0, ErrInvalidDecimal
	}
	whole, frac, hasDot := strings.Cut(s, ".")
	if whole == "" || (hasDot && frac == "") {
		return 0, ErrInvalidDecimal
	}
	if len(frac) > scale {
		// Trailing zeros beyond the scale carry no value.
		if strings.Trim(frac[scale:], "0") != "" {
			return 0, ErrTooPrecise
		}
		frac = frac[:scale]
	}
	var out int64
	for _, digits := range []string{whole, frac + strings.Repeat("0", scale-len(frac))} {
		for _, r := range digits {
			if r < '0' || r > '9' {
				return 0, ErrInvalidDecimal
			}
			d := int64(r - '0')
			if out > (math.MaxInt64-d)/10 {
				return 0, ErrOverflow
			}
			out = out*10 + d
		}
	}
	return out, nil
}

// BlendedPrice weights input and output prices 3:1, the usual chat workload
// mix. Integer division truncates toward zero; the inputs are micro-USD so
// the truncated part is below one micro-USD.
func BlendedPrice(input, output int64) (int64, error) {
	if input < 0 || output < 0 {
		return 0, ErrInvalidDecimal
	}
	if input > (math.MaxInt64-output)/3 {
		return 0, ErrOverflow
	}
	return (3*input + output) / 4, nil
}

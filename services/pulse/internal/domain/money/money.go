// Package money defines fixed-point quantities used by Pulse accounting.
package money

import (
	"errors"
	"math/big"
)

// Milli is the smallest contribution unit used by the accounting path.
// 1 contribution = 1,000 milli-contribution.
type Milli int64

// Bps is a multiplier in basis points. 10,000 bps means 1.00x.
type Bps int32

const (
	MilliPerContribution Milli = 1000
	BpsOne               Bps   = 10000
	MaxBps               Bps   = 1000000
	maxMilli             Milli = 1<<63 - 1
	minMilli             Milli = -maxMilli - 1
)

var (
	ErrOverflow   = errors.New("fixed-point value overflow")
	ErrInvalidBps = errors.New("basis points must be between 0 and 1000000")
)

func (m Milli) Add(other Milli) (Milli, error) {
	if other > 0 && m > maxMilli-other {
		return 0, ErrOverflow
	}
	if other < 0 && m < minMilli-other {
		return 0, ErrOverflow
	}
	return m + other, nil
}

func (m Milli) Sub(other Milli) (Milli, error) {
	if other == minMilli {
		if m >= 0 {
			return 0, ErrOverflow
		}
		return m - other, nil
	}
	return m.Add(-other)
}

// MultiplyBps calculates m * multiplier / 10000 without floating point. It
// uses integer truncation toward zero and rejects overflow of the final value.
// big.Int is intentional here: checking the final quotient avoids rejecting
// valid values merely because the intermediate product exceeds int64.
func (m Milli) MultiplyBps(multiplier Bps) (Milli, error) {
	if err := multiplier.Validate(); err != nil {
		return 0, err
	}
	product := new(big.Int).Mul(big.NewInt(int64(m)), big.NewInt(int64(multiplier)))
	quotient := new(big.Int).Quo(product, big.NewInt(int64(BpsOne)))
	if !quotient.IsInt64() {
		return 0, ErrOverflow
	}
	return Milli(quotient.Int64()), nil
}

func (b Bps) Validate() error {
	if b < 0 || b > MaxBps {
		return ErrInvalidBps
	}
	return nil
}

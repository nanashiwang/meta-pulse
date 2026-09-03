package money

import (
	"math"
	"testing"
)

func TestMilliMultiplyBpsUsesFixedPoint(t *testing.T) {
	got, err := Milli(1500).MultiplyBps(12000)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1800 {
		t.Fatalf("got %d, want 1800", got)
	}
}

func TestMilliMultiplyBpsSupportsNegativeValues(t *testing.T) {
	got, err := Milli(-1500).MultiplyBps(12000)
	if err != nil {
		t.Fatal(err)
	}
	if got != -1800 {
		t.Fatalf("got %d, want -1800", got)
	}
}

func TestMilliMultiplyBpsChecksFinalOverflow(t *testing.T) {
	if _, err := Milli(math.MaxInt64).MultiplyBps(MaxBps); err != ErrOverflow {
		t.Fatalf("error = %v, want %v", err, ErrOverflow)
	}
	if got, err := Milli(math.MaxInt64).MultiplyBps(1); err != nil || got != 922337203685477 {
		t.Fatalf("got %d/%v, want 922337203685477/nil", got, err)
	}
}

func TestMilliAddAndSubCheckOverflow(t *testing.T) {
	if _, err := Milli(math.MaxInt64).Add(1); err != ErrOverflow {
		t.Fatalf("add error = %v, want %v", err, ErrOverflow)
	}
	if _, err := Milli(math.MinInt64).Sub(1); err != ErrOverflow {
		t.Fatalf("sub error = %v, want %v", err, ErrOverflow)
	}
	if got, err := Milli(-10).Sub(math.MinInt64); err != nil || got != math.MaxInt64-9 {
		t.Fatalf("sub got %d/%v", got, err)
	}
}

func TestBpsRejectsUnboundedMultiplier(t *testing.T) {
	if err := Bps(MaxBps + 1).Validate(); err == nil {
		t.Fatal("invalid bps accepted")
	}
}

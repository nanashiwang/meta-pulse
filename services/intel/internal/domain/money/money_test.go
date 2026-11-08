package money

import (
	"errors"
	"testing"
)

func TestParseScaled(t *testing.T) {
	cases := []struct {
		in    string
		scale int
		want  int64
		err   error
	}{
		{"2.50", MicroScale, 2_500_000, nil},
		{"0", MicroScale, 0, nil},
		{"15", MicroScale, 15_000_000, nil},
		{"0.000001", MicroScale, 1, nil},
		{"87.3", BpsScale, 8730, nil},
		{"87.300", BpsScale, 8730, nil},
		{"87.305", BpsScale, 0, ErrTooPrecise},
		{"123.4", MilliScale, 123_400, nil},
		{"-1", MicroScale, 0, ErrInvalidDecimal},
		{"1e3", MicroScale, 0, ErrInvalidDecimal},
		{"1,000", MicroScale, 0, ErrInvalidDecimal},
		{".5", MicroScale, 0, ErrInvalidDecimal},
		{"5.", MicroScale, 0, ErrInvalidDecimal},
		{"", MicroScale, 0, ErrInvalidDecimal},
		{"99999999999999999999", MicroScale, 0, ErrOverflow},
	}
	for _, c := range cases {
		got, err := ParseScaled(c.in, c.scale)
		if !errors.Is(err, c.err) || got != c.want {
			t.Errorf("ParseScaled(%q, %d) = %d, %v; want %d, %v", c.in, c.scale, got, err, c.want, c.err)
		}
	}
}

func TestBlendedPrice(t *testing.T) {
	got, err := BlendedPrice(2_000_000, 10_000_000)
	if err != nil || got != 4_000_000 {
		t.Fatalf("BlendedPrice = %d, %v; want 4000000", got, err)
	}
	if _, err := BlendedPrice(-1, 0); err == nil {
		t.Fatal("negative input accepted")
	}
	if _, err := BlendedPrice(1<<62, 1<<62); !errors.Is(err, ErrOverflow) {
		t.Fatalf("overflow not detected: %v", err)
	}
}

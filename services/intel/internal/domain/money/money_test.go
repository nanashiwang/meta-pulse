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

func TestRoundDecimal(t *testing.T) {
	cases := map[string]string{"2.5": "2.5", "15": "15", "1e-06": "0.000001", "0.0000015": "0.000002", "94.2": "94.2", "87.305": "87.31", "0": "0", "1.10": "1.1"}
	for in, want := range cases {
		scale := MicroScale
		if in == "87.305" {
			scale = BpsScale
		}
		got, err := RoundDecimal(in, scale)
		if err != nil || got != want {
			t.Errorf("RoundDecimal(%q, %d) = %q, %v; want %q", in, scale, got, err, want)
		}
		if _, err := ParseScaled(got, scale); err != nil {
			t.Errorf("result %q does not parse back: %v", got, err)
		}
	}
	for _, bad := range []string{"-1", "abc", "", "NaN"} {
		if _, err := RoundDecimal(bad, 2); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

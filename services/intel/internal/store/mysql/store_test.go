package mysql

import (
	"testing"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/money"
)

func TestMedian(t *testing.T) {
	cases := []struct {
		in   []int64
		want int64
	}{
		{nil, 0},
		{[]int64{5}, 5},
		{[]int64{9, 1, 5}, 5},
		{[]int64{1, 4}, 2},
		{[]int64{10, 2, 8, 4}, 6},
	}
	for _, c := range cases {
		if got := Median(c.in); got != c.want {
			t.Errorf("Median(%v) = %d, want %d", c.in, got, c.want)
		}
	}
	in := []int64{3, 1, 2}
	Median(in)
	if in[0] != 3 {
		t.Fatal("Median must not reorder its input")
	}
}

func TestOpenRequiresDSN(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("empty DSN accepted")
	}
	if _, err := Open("not a dsn"); err == nil {
		t.Fatal("malformed DSN accepted")
	}
}

func TestFormatScaledRoundTrips(t *testing.T) {
	cases := map[int64]string{0: "0", 2_500_000: "2.5", 15_000_000: "15", 1: "0.000001", 123_456_789: "123.456789"}
	for v, want := range cases {
		if got := FormatMicro(v); got != want {
			t.Errorf("FormatMicro(%d) = %q, want %q", v, got, want)
		}
		back, err := money.ParseScaled(FormatMicro(v), money.MicroScale)
		if err != nil || back != v {
			t.Errorf("round trip %d -> %q -> %d, %v", v, FormatMicro(v), back, err)
		}
	}
	if formatScaled(8730, 2) != "87.3" || formatScaled(123400, 3) != "123.4" {
		t.Fatal("bps/milli formatting")
	}
}

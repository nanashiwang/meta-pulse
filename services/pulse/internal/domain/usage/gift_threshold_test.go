package usage

import "testing"

func TestGiftThreshold(t *testing.T) {
	for _, tc := range []struct {
		value, unit string
		want        int64
	}{{"1", "wan", 10000}, {"1.5", "wan", 15000}, {"1", "yi", 100000000}, {"0.00000001", "yi", 1}, {"92233720368.54775807", "yi", 9223372036854775807}} {
		v, e := GiftUsageThreshold(tc.value, tc.unit)
		if e != nil || v != tc.want {
			t.Fatal(tc, v, e)
		}
		if ExceedsGiftThreshold(&v, v) || ExceedsGiftThreshold(nil, v) {
			t.Fatal("equal or missing passed")
		}
		if !ExceedsGiftThreshold(&v, v - 1) {
			t.Fatal("one above failed")
		}
	}
	for _, v := range []string{"", "1e3", "-1", "0.00001", "922337203685478"} {
		if _, e := GiftUsageThreshold(v, "wan"); e == nil {
			t.Fatal(v)
		}
	}
}

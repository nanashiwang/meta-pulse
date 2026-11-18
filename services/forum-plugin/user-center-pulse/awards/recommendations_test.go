package awards

import "testing"

func TestRecommendationShares(t *testing.T) {
	for i, want := range []int{2000, 2000, 2000, 2000, 2000, 2000, 2100, 2400, 2700, 3000} {
		got := recommendationShares(i + 1)
		if len(got) != i+1 {
			t.Fatalf("%d: %v", i+1, got)
		}
		sum := 0
		for _, v := range got {
			if v < 300 || got[0]-v > 1 {
				t.Fatalf("invalid shares: %v", got)
			}
			sum += v
		}
		if sum != want {
			t.Fatalf("%d: %d wanted %d", i+1, sum, want)
		}
	}
	if recommendationShares(5)[0] != 400 || recommendationShares(0) != nil || recommendationShares(11) != nil {
		t.Fatal("boundary shares invalid")
	}
}

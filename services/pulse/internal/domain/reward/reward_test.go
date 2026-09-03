package reward

import "testing"

func TestDeriveIsStable(t *testing.T) {
	first, err := Derive([]byte("secret"), 1, 2, "action", "v1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Derive([]byte("secret"), 1, 2, "action", "v1")
	if err != nil || first != second {
		t.Fatalf("first=%x second=%x err=%v", first, second, err)
	}
	other, err := Derive([]byte("secret"), 1, 2, "other", "v1")
	if err != nil || first == other {
		t.Fatalf("action id did not affect random: first=%x other=%x", first, other)
	}
}

func TestSelectWeightedIsDeterministicAndSkipsDisabled(t *testing.T) {
	defs := []Definition{
		{ID: 1, RewardKey: "disabled", RewardType: "quota", Amount: 1, Weight: 100, ConfigVersion: "v1", Enabled: false},
		{ID: 2, RewardKey: "one", RewardType: "quota", Amount: 2, Weight: 1, ConfigVersion: "v1", Enabled: true},
	}
	selected, err := SelectWeighted(defs, [32]byte{255})
	if err != nil || selected.ID != 2 {
		t.Fatalf("selected=%+v err=%v", selected, err)
	}
	selectedAgain, err := SelectWeighted(defs, [32]byte{255})
	if err != nil || selected != selectedAgain {
		t.Fatalf("selection not stable: %+v %+v", selected, selectedAgain)
	}
}

func TestSelectWeightedRejectsNoValidDefinition(t *testing.T) {
	_, err := SelectWeighted([]Definition{{ID: 1, RewardKey: "bad", RewardType: "quota", Amount: -1, Weight: 1, ConfigVersion: "v1", Enabled: true}}, [32]byte{})
	if err != ErrNoDefinition {
		t.Fatalf("err=%v, want %v", err, ErrNoDefinition)
	}
}

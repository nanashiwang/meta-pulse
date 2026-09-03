package experiment

import "testing"

func TestAssignIsStable(t *testing.T) {
	variants := []Variant{{Name: "control", Percentage: 5000}, {Name: "treatment", Percentage: 5000}}
	first, err := Assign([]byte("secret"), "exp-1", 7, variants)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Assign([]byte("secret"), "exp-1", 7, variants)
	if err != nil || first != second {
		t.Fatalf("first=%+v second=%+v err=%v", first, second, err)
	}
	if _, err := Assign([]byte("secret"), "exp-1", 7, []Variant{{Name: "control", Percentage: 10001}}); err == nil {
		t.Fatal("invalid percentages accepted")
	}
}

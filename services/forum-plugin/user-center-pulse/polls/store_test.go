package polls

import (
	"strings"
	"testing"
)

func config() *Config {
	return &Config{Title: "Best tool?", Options: []string{"A", "B", "C"}, MaxChoices: 2, Days: 7}
}
func TestNormalize(t *testing.T) {
	c, err := Normalize(Command{Op: "create", QuestionID: "1001", Config: config()})
	if err != nil || c.Config.Days != 7 {
		t.Fatal(err)
	}
	bad := []Command{{Op: "create", QuestionID: "1", Config: &Config{Title: "x", Options: []string{"a", " A "}, MaxChoices: 1, Days: 7}}, {Op: "create", QuestionID: "1", Config: &Config{Title: strings.Repeat("题", 161), Options: []string{"a", "b"}, MaxChoices: 1, Days: 7}}, {Op: "vote", QuestionID: "1", Choices: []int{0, 0}}, {Op: "vote", QuestionID: "1", Choices: []int{-1}}, {Op: "vote", QuestionID: "1", Choices: []int{10}}, {Op: "vote", QuestionID: "1", Choices: []int{}}, {Op: "vote", QuestionID: "1", Choices: []int{1}, Version: -1}, {Op: "close", QuestionID: "0"}, {Op: "edit", QuestionID: "1"}}
	for _, c := range bad {
		if _, err := Normalize(c); err != ErrInvalid {
			t.Fatalf("accepted %+v", c)
		}
	}
	choices := []int{2, 0}
	v, err := Normalize(Command{Op: "vote", QuestionID: "1", Choices: choices})
	if err != nil || v.Choices[0] != 0 || choices[0] != 2 {
		t.Fatal("normalization must be deterministic without mutating caller")
	}
}

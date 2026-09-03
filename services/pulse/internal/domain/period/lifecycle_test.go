package period

import "testing"

func TestTransitionOnlyAllowsForwardLifecycle(t *testing.T) {
	for _, pair := range [][2]Status{{StatusDraft, StatusActive}, {StatusActive, StatusSettling}, {StatusSettling, StatusClosed}} {
		if err := Transition(pair[0], pair[1]); err != nil {
			t.Fatalf("%s -> %s: %v", pair[0], pair[1], err)
		}
	}
	if err := Transition(StatusClosed, StatusActive); err == nil {
		t.Fatal("closed period was allowed to reopen")
	}
}

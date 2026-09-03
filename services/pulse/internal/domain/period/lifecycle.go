package period

import "fmt"

// Transition validates the only lifecycle edges allowed by the campaign
// state machine. Close is intentionally idempotent at the service layer.
func Transition(from, to Status) error {
	allowed := map[Status]Status{
		StatusDraft:    StatusActive,
		StatusActive:   StatusSettling,
		StatusSettling: StatusClosed,
	}
	if allowed[from] != to {
		return fmt.Errorf("invalid period transition %s -> %s", from, to)
	}
	return nil
}

package service

import "errors"

var errWorkerCursorAdvanced = errors.New("worker cursor advanced concurrently")

func cursorAdvanced(current, incoming string, expected []string) bool {
	return len(expected) == 1 && current != expected[0] && current != incoming
}

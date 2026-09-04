package service

import (
	"fmt"
	"unicode/utf8"
)

// validDBText mirrors MySQL VARCHAR character limits while rejecting malformed
// UTF-8 before values reach a persistence adapter. MySQL's utf8mb4 VARCHAR
// limits are expressed in characters, not bytes.
func validDBText(value string, maxCharacters int) bool {
	return maxCharacters > 0 && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxCharacters
}

func requireDBText(name, value string, maxCharacters int) error {
	if !validDBText(value, maxCharacters) {
		return fmt.Errorf("%s must be valid UTF-8 and at most %d characters", name, maxCharacters)
	}
	return nil
}

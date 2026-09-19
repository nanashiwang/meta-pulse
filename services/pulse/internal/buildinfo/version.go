// Package buildinfo exposes release identity without loading runtime secrets.
package buildinfo

import (
	"fmt"
	"os"
)

var Version = "dev"
var Revision = "unknown"

// PrintVersion handles the standalone --version flag before config or DB access.
func PrintVersion() bool {
	if len(os.Args) != 2 || os.Args[1] != "--version" {
		return false
	}
	fmt.Printf("Meta Pulse %s (%s)\n", Version, Revision)
	return true
}

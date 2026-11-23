// Package rewarddelivery partitions receipt IDs for the shared reward pipeline.
package rewarddelivery

import (
	"crypto/sha256"
	"encoding/binary"
)

// AwardBase separates content awards from limited-gift round IDs. Both ranges
// fit in signed 64-bit SQL and are transported as exact integers on the server.
const AwardBase = 1 << 62

func AwardID(source string) uint64 {
	sum := sha256.Sum256([]byte("metar-content-award-v1:" + source))
	return AwardBase | (binary.BigEndian.Uint64(sum[:8]) & (AwardBase - 1))
}

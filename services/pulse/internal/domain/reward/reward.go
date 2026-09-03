// Package reward contains deterministic, versioned reward selection rules.
package reward

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var (
	ErrNoDefinition   = errors.New("no enabled reward definition")
	ErrInvalidSecret  = errors.New("reward random secret is empty")
	ErrWeightOverflow = errors.New("reward weight overflow")
)

type Definition struct {
	ID                uint64
	RewardKey         string
	RewardType        string
	Amount            int64
	Weight            uint64
	TransferableQuota bool
	ConfigVersion     string
	Enabled           bool
}

func (d Definition) Valid() bool {
	return d.ID > 0 && strings.TrimSpace(d.RewardKey) != "" && strings.TrimSpace(d.RewardType) != "" && d.Amount >= 0 && d.Weight > 0 && strings.TrimSpace(d.ConfigVersion) != ""
}

// Derive returns an HMAC-SHA256 random result. All inputs that define an
// action and its immutable economics version are included in the message.
func Derive(secret []byte, periodID, userID uint64, actionID, configVersion string) ([32]byte, error) {
	if len(secret) == 0 {
		return [32]byte{}, ErrInvalidSecret
	}
	if periodID == 0 || userID == 0 || strings.TrimSpace(actionID) == "" || strings.TrimSpace(configVersion) == "" {
		return [32]byte{}, errors.New("invalid reward random input")
	}
	message := strings.Join([]string{strconv.FormatUint(periodID, 10), strconv.FormatUint(userID, 10), actionID, configVersion}, "\n")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(message))
	var result [32]byte
	copy(result[:], mac.Sum(nil))
	return result, nil
}

func RandomHex(value [32]byte) string { return hex.EncodeToString(value[:]) }

// SelectWeighted maps the stable random bytes to a weighted definition. It
// intentionally ignores disabled/invalid definitions and never uses floats.
func SelectWeighted(definitions []Definition, random [32]byte) (Definition, error) {
	var total uint64
	valid := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		if !definition.Enabled || !definition.Valid() {
			continue
		}
		if math.MaxUint64-total < definition.Weight {
			return Definition{}, ErrWeightOverflow
		}
		total += definition.Weight
		valid = append(valid, definition)
	}
	if total == 0 {
		return Definition{}, ErrNoDefinition
	}
	// Rejection sampling avoids modulo bias while keeping the result fully
	// deterministic for a given random digest.
	n := binaryUint64(random[:8])
	limit := math.MaxUint64 - (math.MaxUint64 % total)
	for n >= limit {
		n = binaryUint64(random[8:16])
		random = sha256.Sum256(random[:])
	}
	target := n % total
	var cumulative uint64
	for _, definition := range valid {
		cumulative += definition.Weight
		if target < cumulative {
			return definition, nil
		}
	}
	return valid[len(valid)-1], nil
}

func binaryUint64(value []byte) uint64 {
	if len(value) < 8 {
		return 0
	}
	return uint64(value[0])<<56 | uint64(value[1])<<48 | uint64(value[2])<<40 | uint64(value[3])<<32 |
		uint64(value[4])<<24 | uint64(value[5])<<16 | uint64(value[6])<<8 | uint64(value[7])
}

func GrantID(periodID, userID uint64, actionID string) string {
	value := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", periodID, userID, actionID)))
	return "pg_" + hex.EncodeToString(value[:])[:48]
}

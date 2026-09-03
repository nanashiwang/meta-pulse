// Package experiment provides stable, deterministic holdout assignment.
package experiment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
)

var ErrInvalidAssignment = errors.New("invalid experiment assignment")

type Variant struct {
	Name       string
	Percentage uint16 // basis points, total must be <= 10000
}

type Assignment struct {
	ExperimentID string
	UserID       uint64
	Cohort       string
	BucketBps    uint16
}

func Assign(secret []byte, experimentID string, userID uint64, variants []Variant) (Assignment, error) {
	if len(secret) == 0 || strings.TrimSpace(experimentID) == "" || userID == 0 || len(variants) == 0 {
		return Assignment{}, ErrInvalidAssignment
	}
	var total uint32
	for _, variant := range variants {
		if strings.TrimSpace(variant.Name) == "" || variant.Percentage == 0 {
			return Assignment{}, ErrInvalidAssignment
		}
		total += uint32(variant.Percentage)
		if total > 10000 {
			return Assignment{}, ErrInvalidAssignment
		}
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(experimentID))
	_, _ = mac.Write([]byte{0})
	var user [8]byte
	binary.BigEndian.PutUint64(user[:], userID)
	_, _ = mac.Write(user[:])
	digest := mac.Sum(nil)
	bucket := binary.BigEndian.Uint16(digest[:2]) % 10000
	var cumulative uint16
	for _, variant := range variants {
		cumulative += variant.Percentage
		if bucket < cumulative {
			return Assignment{ExperimentID: experimentID, UserID: userID, Cohort: variant.Name, BucketBps: bucket}, nil
		}
	}
	return Assignment{ExperimentID: experimentID, UserID: userID, Cohort: "holdout", BucketBps: bucket}, nil
}

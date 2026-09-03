// Package usage defines the normalized event exchanged by all ingest modes.
package usage

import (
	"errors"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/money"
)

type EventType string

const (
	EventConsume EventType = "consume"
	EventRefund  EventType = "refund"
	EventCorrect EventType = "correction"
)

type Status string

const (
	StatusAccepted     Status = "accepted"
	StatusManualReview Status = "manual_review"
)

var (
	ErrUnsupportedLog = errors.New("unsupported new-api log type")
	ErrNeedsReview    = errors.New("usage event needs manual review")
)

type Event struct {
	ID                   uint64
	SourceSystem         string
	SourceEventID        string
	CursorValue          string
	PayloadHash          string
	UserID               uint64
	PeriodID             uint64
	EventType            EventType
	SourceCreatedAt      time.Time
	QuotaDelta           int64
	ModelName            string
	ChannelID            uint64
	RequestID            string
	RelatedSourceEventID string
	NeedsReview          bool
	ReviewReason         string
	Eligible             bool
	EconomicsRuleID      *uint64
	MultiplierBps        money.Bps
	ContributionMilli    money.Milli
	Status               Status
}

package newapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
)

type UsageMapper struct {
	SourceSystem string
}

func (m UsageMapper) Map(record LogRecord) (usage.Event, error) {
	if record.ID <= 0 || record.UserID <= 0 || record.CreatedAt <= 0 {
		return usage.Event{}, fmt.Errorf("invalid new-api log identity")
	}
	sourceSystem := m.SourceSystem
	if sourceSystem == "" {
		sourceSystem = "new-api-log"
	}
	hash, err := record.PayloadHash()
	if err != nil {
		return usage.Event{}, err
	}
	event := usage.Event{
		SourceSystem:    sourceSystem,
		SourceEventID:   strconv.FormatInt(record.ID, 10),
		CursorValue:     Cursor{CreatedAt: record.CreatedAt, ID: record.ID}.String(),
		PayloadHash:     hash,
		UserID:          uint64(record.UserID),
		SourceCreatedAt: sourceTime(record.CreatedAt),
		ModelName:       record.ModelName,
		ChannelID:       uint64(maxInt64(record.ChannelID)),
		RequestID:       record.RequestID,
	}
	switch record.Type {
	case LogTypeConsume:
		if record.Quota <= 0 {
			event.NeedsReview = true
			event.ReviewReason = "consume log has non-positive quota"
			return event, nil
		}
		event.EventType = usage.EventConsume
		event.QuotaDelta = record.Quota
	case LogTypeRefund:
		if record.Quota <= 0 {
			event.NeedsReview = true
			event.ReviewReason = "refund log has non-positive quota"
			return event, nil
		}
		event.EventType = usage.EventRefund
		event.QuotaDelta = -record.Quota
		event.RelatedSourceEventID = relatedSourceEventID(record)
		if event.RelatedSourceEventID == "" && strings.TrimSpace(record.RequestID) == "" {
			event.NeedsReview = true
			event.ReviewReason = "refund has no stable consume correlation"
		}
	default:
		return usage.Event{}, usage.ErrUnsupportedLog
	}

	// Only the paid portion of an immutable new-api receipt may earn tickets.
	proof, paid, err := verifiedPaidFunding(record)
	if err != nil {
		event.NeedsReview = true
		event.ReviewReason = "paid funding proof missing or invalid"
		event.QuotaDelta = 0
	} else {
		event.FundingProof = proof
		event.QuotaDelta = paid
	}
	return event, nil
}

func relatedSourceEventID(record LogRecord) string {
	if strings.TrimSpace(record.Other) == "" {
		return ""
	}
	// UseNumber is required here: log IDs are int64 values and decoding them
	// through float64 can silently change IDs above 2^53, causing a refund to
	// be correlated with the wrong consume event. Strictly consume one JSON
	// value so malformed metadata cannot be partially interpreted.
	decoder := json.NewDecoder(bytes.NewReader([]byte(record.Other)))
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil {
		return ""
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ""
	}
	for _, key := range []string{"origin_log_id", "original_log_id", "consume_log_id", "related_log_id", "log_id"} {
		if value, ok := fields[key]; ok {
			if id := scalarID(value); id != "" {
				return id
			}
		}
	}
	return ""
}

func scalarID(value any) string {
	var raw string
	switch v := value.(type) {
	case string:
		raw = strings.TrimSpace(v)
	case json.Number:
		raw = v.String()
	default:
		return ""
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 {
		return ""
	}
	return strconv.FormatInt(parsed, 10)
}

func maxInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func sha256Hex(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func sourceTime(unixSeconds int64) time.Time {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	return time.Unix(unixSeconds, 0).In(location)
}

// Decode only the allowlisted funding snapshot; raw Other is never persisted.
func verifiedPaidFunding(record LogRecord) (string, int64, error) {
	var envelope struct {
		Funding struct {
			Version int    `json:"version"`
			Status  string `json:"status"`
			Paid    int64  `json:"paid_quota"`
			NonPaid int64  `json:"non_paid_quota"`
			Unknown int64  `json:"unknown_quota"`
			Proof   string `json:"proof_ref"`
		} `json:"pulse_funding"`
	}
	if record.Type != LogTypeConsume || record.RequestID == "" {
		return "", 0, usage.ErrNeedsReview
	}
	decoder := json.NewDecoder(strings.NewReader(record.Other))
	if err := decoder.Decode(&envelope); err != nil {
		return "", 0, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", 0, usage.ErrNeedsReview
	}
	f := envelope.Funding
	if f.Version != 1 || f.Status != "verified" || len(f.Proof) == 0 || len(f.Proof) > 191 || strings.ContainsAny(f.Proof, "\r\n") || f.Paid < 0 || f.NonPaid < 0 || f.Unknown < 0 || f.Paid > record.Quota || f.NonPaid > record.Quota-f.Paid || f.Unknown != record.Quota-f.Paid-f.NonPaid {
		return "", 0, usage.ErrNeedsReview
	}
	return f.Proof, f.Paid, nil
}

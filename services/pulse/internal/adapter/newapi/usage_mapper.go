package newapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	return event, nil
}

func relatedSourceEventID(record LogRecord) string {
	var fields map[string]any
	if json.Unmarshal([]byte(record.Other), &fields) == nil {
		for _, key := range []string{"origin_log_id", "original_log_id", "consume_log_id", "related_log_id", "log_id"} {
			if value, ok := fields[key]; ok {
				if id := scalarID(value); id != "" {
					return id
				}
			}
		}
	}
	return ""
}

func scalarID(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v > 0 && v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
	case json.Number:
		return v.String()
	}
	return ""
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

package newapi

import (
	"testing"

	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
)

func TestMapperNormalizesConsumeWithoutPersistingSensitiveContent(t *testing.T) {
	record := LogRecord{ID: 10, UserID: 7, CreatedAt: 1_700_000_000, Type: LogTypeConsume, Quota: 500, ModelName: "gpt-4o", ChannelID: 2, Other: `{"request_id":"req-1"}`}
	event, err := (UsageMapper{}).Map(record)
	if err != nil {
		t.Fatal(err)
	}
	if event.EventType != usage.EventConsume || event.QuotaDelta != 500 || len(event.PayloadHash) != 64 {
		t.Fatalf("event = %+v", event)
	}
}

func TestMapperMarksUncorrelatedRefundForReview(t *testing.T) {
	event, err := (UsageMapper{}).Map(LogRecord{ID: 11, UserID: 7, CreatedAt: 1_700_000_000, Type: LogTypeRefund, Quota: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !event.NeedsReview || event.ReviewReason == "" {
		t.Fatalf("event = %+v", event)
	}
}

func TestMapperReadsTaskRefundCorrelation(t *testing.T) {
	event, err := (UsageMapper{}).Map(LogRecord{ID: 12, UserID: 7, CreatedAt: 1_700_000_000, Type: LogTypeRefund, Quota: 100, Other: `{"origin_log_id":10}`})
	if err != nil {
		t.Fatal(err)
	}
	if event.NeedsReview || event.RelatedSourceEventID != "10" || event.QuotaDelta != -100 {
		t.Fatalf("event = %+v", event)
	}
}

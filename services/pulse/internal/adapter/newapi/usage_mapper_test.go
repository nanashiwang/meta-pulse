package newapi

import (
	"testing"

	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
)

func TestMapperNormalizesConsumeWithoutPersistingSensitiveContent(t *testing.T) {
	record := LogRecord{ID: 10, UserID: 7, CreatedAt: 1_700_000_000, Type: LogTypeConsume, Quota: 500, ModelName: "gpt-4o", ChannelID: 2, RequestID: "req-1", Other: `{"pulse_funding":{"version":1,"status":"verified","paid_quota":500,"non_paid_quota":0,"unknown_quota":0,"proof_ref":"proof-1"}}`}
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
	if !event.NeedsReview || event.RelatedSourceEventID != "10" || event.QuotaDelta != 0 {
		t.Fatalf("event = %+v", event)
	}
}

func TestMapperUsesRequestIDForAsyncRefundCorrelation(t *testing.T) {
	event, err := (UsageMapper{}).Map(LogRecord{
		ID: 13, UserID: 7, CreatedAt: 1_700_000_000, Type: LogTypeRefund,
		Quota: 100, RequestID: "request-task-1", Other: `{"task_id":"task-1"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !event.NeedsReview || event.RequestID != "request-task-1" || event.RelatedSourceEventID != "" || event.QuotaDelta != 0 {
		t.Fatalf("event = %+v", event)
	}
}

func TestMapperPreservesLargeNumericRefundCorrelation(t *testing.T) {
	const originID = "9007199254740993"
	event, err := (UsageMapper{}).Map(LogRecord{
		ID: 14, UserID: 7, CreatedAt: 1_700_000_000, Type: LogTypeRefund,
		Quota: 100, Other: `{"origin_log_id":9007199254740993}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !event.NeedsReview || event.RelatedSourceEventID != originID {
		t.Fatalf("event = %+v", event)
	}
}

func TestMapperRejectsUnsafeRefundCorrelationValues(t *testing.T) {
	for _, other := range []string{
		`{"origin_log_id":9007199254740993.5}`,
		`{"origin_log_id":-10}`,
		`{"origin_log_id":"not-a-log-id"}`,
		`{"origin_log_id":10}{"unexpected":true}`,
	} {
		event, err := (UsageMapper{}).Map(LogRecord{
			ID: 15, UserID: 7, CreatedAt: 1_700_000_000, Type: LogTypeRefund,
			Quota: 100, Other: other,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !event.NeedsReview || event.RelatedSourceEventID != "" {
			t.Fatalf("other=%s event=%+v", other, event)
		}
	}
}

func TestMapperOnlyCreditsProvenPaidConsumption(t *testing.T) {
	cases := []struct {
		name, other string
		paid        int64
		review      bool
	}{
		{"legacy", `{}`, 0, true},
		{"unknown", `{"pulse_funding":{"version":1,"status":"unknown","paid_quota":500,"proof_ref":"fake"}}`, 0, true},
		{"partial", `{"pulse_funding":{"version":1,"status":"verified","paid_quota":120,"non_paid_quota":80,"unknown_quota":300,"proof_ref":"proof-1"}}`, 120, false},
		{"gift", `{"pulse_funding":{"version":1,"status":"verified","paid_quota":0,"non_paid_quota":500,"unknown_quota":0,"proof_ref":"proof-2"}}`, 0, false},
		{"mismatch", `{"pulse_funding":{"version":1,"status":"verified","paid_quota":500,"non_paid_quota":1,"unknown_quota":0,"proof_ref":"proof-3"}}`, 0, true},
		{"no proof", `{"pulse_funding":{"version":1,"status":"verified","paid_quota":500}}`, 0, true},
		{"fraction", `{"pulse_funding":{"version":1,"status":"verified","paid_quota":1.5,"proof_ref":"proof-4"}}`, 0, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			event, err := (UsageMapper{}).Map(LogRecord{ID: 99, UserID: 7, CreatedAt: 1700000000, Type: LogTypeConsume, Quota: 500, RequestID: "request", Other: tt.other})
			if err != nil || event.QuotaDelta != tt.paid || event.NeedsReview != tt.review {
				t.Fatalf("event=%+v err=%v", event, err)
			}
		})
	}
}

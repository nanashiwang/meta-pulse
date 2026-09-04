package mysql

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

func TestContentBusinessDayBoundsUseAsiaShanghai(t *testing.T) {
	start, end := contentBusinessDayBounds(time.Date(2026, 9, 3, 16, 0, 0, 0, time.UTC))
	if start.Format(time.RFC3339) != "2026-09-04T00:00:00+08:00" || end.Format(time.RFC3339) != "2026-09-05T00:00:00+08:00" {
		t.Fatalf("bounds=%s..%s", start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	previous, _ := contentBusinessDayBounds(time.Date(2026, 9, 3, 15, 59, 59, 0, time.UTC))
	if previous.Format(time.RFC3339) != "2026-09-03T00:00:00+08:00" {
		t.Fatalf("previous start=%s", previous.Format(time.RFC3339))
	}
}

func TestContentAwardTransitionOnlyAllowsSettledToReversed(t *testing.T) {
	repo := &contentRepository{}
	cases := []struct {
		actionID string
		from     string
		to       string
	}{
		{"", ports.ContentAwardSettled, ports.ContentAwardReversed},
		{"award", ports.ContentAwardPending, ports.ContentAwardReversed},
		{"award", ports.ContentAwardSettled, ports.ContentAwardPending},
		{"award", ports.ContentAwardReversed, ports.ContentAwardSettled},
	}
	for _, test := range cases {
		if err := repo.TransitionAwardStatus(context.Background(), test.actionID, test.from, test.to); err == nil {
			t.Fatalf("transition %q %s -> %s was accepted", test.actionID, test.from, test.to)
		}
	}
}

func TestParseAggregateInt64RejectsOverflow(t *testing.T) {
	valid := map[string]int64{
		"0":                    0,
		"9223372036854775807":  9223372036854775807,
		"-9223372036854775808": -9223372036854775808,
	}
	for raw, want := range valid {
		got, err := parseAggregateInt64(raw)
		if err != nil || got != want {
			t.Fatalf("raw=%q got=%d err=%v want=%d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "9223372036854775808", "-9223372036854775809", "1.5", "not-a-number"} {
		if _, err := parseAggregateInt64(raw); err == nil {
			t.Fatalf("invalid aggregate %q was accepted", raw)
		}
	}
}

func TestAuditLogCreateValidatesDurableShape(t *testing.T) {
	now := time.Now()
	valid := ports.AuditLog{
		ActorType: "admin", ActorID: "operator-1", Action: "ledger_adjustment",
		ResourceType: "ledger_entry", ResourceID: "request-1", Reason: "manual correction",
		BeforeJSON: []byte(`{"balance":1}`), AfterJSON: []byte(`{"balance":2}`),
		RequestID: "request-1", CreatedAt: now,
	}
	if err := validateAuditLogCreate(valid); err != nil {
		t.Fatalf("valid audit log rejected: %v", err)
	}
	withoutOptionalFields := valid
	withoutOptionalFields.BeforeJSON = nil
	withoutOptionalFields.AfterJSON = nil
	withoutOptionalFields.RequestID = ""
	if err := validateAuditLogCreate(withoutOptionalFields); err != nil {
		t.Fatalf("audit log without optional fields rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*ports.AuditLog)
	}{
		{"explicit id", func(v *ports.AuditLog) { v.ID = 1 }},
		{"zero creation time", func(v *ports.AuditLog) { v.CreatedAt = time.Time{} }},
		{"blank actor type", func(v *ports.AuditLog) { v.ActorType = " " }},
		{"padded actor id", func(v *ports.AuditLog) { v.ActorID = " operator-1" }},
		{"malformed utf8", func(v *ports.AuditLog) { v.Action = string([]byte{0xff}) }},
		{"oversized resource type", func(v *ports.AuditLog) { v.ResourceType = strings.Repeat("类", 65) }},
		{"oversized reason", func(v *ports.AuditLog) { v.Reason = strings.Repeat("理", 501) }},
		{"oversized request id", func(v *ports.AuditLog) { v.RequestID = strings.Repeat("r", 129) }},
		{"invalid before json", func(v *ports.AuditLog) { v.BeforeJSON = []byte(`{"balance":`) }},
		{"trailing after json", func(v *ports.AuditLog) { v.AfterJSON = []byte(`{} {}`) }},
		{"oversized after json", func(v *ports.AuditLog) { v.AfterJSON = []byte(`"` + strings.Repeat("a", maxAuditJSONBytes) + `"`) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			value.BeforeJSON = append([]byte(nil), valid.BeforeJSON...)
			value.AfterJSON = append([]byte(nil), valid.AfterJSON...)
			test.mutate(&value)
			if err := validateAuditLogCreate(value); err == nil {
				t.Fatal("invalid audit log was accepted")
			}
		})
	}
}

func TestExperimentAssignmentCreateValidatesImmutableIdentity(t *testing.T) {
	valid := ports.ExperimentAssignment{
		ExperimentID: "holdout-v1", UserID: 7, Cohort: "control",
		BucketBps: 9999, AssignedAt: time.Now(),
	}
	if err := validateExperimentAssignmentCreate(valid); err != nil {
		t.Fatalf("valid experiment assignment rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*ports.ExperimentAssignment)
	}{
		{"explicit id", func(v *ports.ExperimentAssignment) { v.ID = 1 }},
		{"missing user", func(v *ports.ExperimentAssignment) { v.UserID = 0 }},
		{"blank experiment", func(v *ports.ExperimentAssignment) { v.ExperimentID = " " }},
		{"padded experiment", func(v *ports.ExperimentAssignment) { v.ExperimentID = " holdout-v1" }},
		{"long experiment", func(v *ports.ExperimentAssignment) { v.ExperimentID = strings.Repeat("实", 129) }},
		{"malformed cohort", func(v *ports.ExperimentAssignment) { v.Cohort = string([]byte{0xff}) }},
		{"long cohort", func(v *ports.ExperimentAssignment) { v.Cohort = strings.Repeat("组", 33) }},
		{"bucket out of range", func(v *ports.ExperimentAssignment) { v.BucketBps = 10000 }},
		{"zero assignment time", func(v *ports.ExperimentAssignment) { v.AssignedAt = time.Time{} }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.mutate(&value)
			if err := validateExperimentAssignmentCreate(value); err == nil {
				t.Fatal("invalid experiment assignment was accepted")
			}
		})
	}
}

func TestMetricUpsertValidatesDateIdentityAndDimensions(t *testing.T) {
	valid := ports.MetricValue{
		MetricDate: time.Now(), MetricName: "settlement_retry",
		DimensionHash: emptyMetricDimensionsHash,
	}
	if err := validateMetricUpsert(valid); err != nil {
		t.Fatalf("valid metric rejected: %v", err)
	}
	dimensions := []byte(`{"region":"cn"}`)
	dimensionsHash, err := canonicalSettlementPayloadHash(dimensions)
	if err != nil {
		t.Fatal(err)
	}
	withDimensions := valid
	withDimensions.DimensionHash = dimensionsHash
	withDimensions.Dimensions = dimensions
	if err := validateMetricUpsert(withDimensions); err != nil {
		t.Fatalf("valid dimensioned metric rejected: %v", err)
	}

	cases := []struct {
		name   string
		base   ports.MetricValue
		mutate func(*ports.MetricValue)
	}{
		{"zero date", valid, func(v *ports.MetricValue) { v.MetricDate = time.Time{} }},
		{"date before mysql range", valid, func(v *ports.MetricValue) { v.MetricDate = time.Date(999, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"blank name", valid, func(v *ports.MetricValue) { v.MetricName = " " }},
		{"long name", valid, func(v *ports.MetricValue) { v.MetricName = strings.Repeat("指", 129) }},
		{"malformed name", valid, func(v *ports.MetricValue) { v.MetricName = string([]byte{0xff}) }},
		{"short hash", valid, func(v *ports.MetricValue) { v.DimensionHash = "abcd" }},
		{"uppercase hash", valid, func(v *ports.MetricValue) { v.DimensionHash = strings.ToUpper(emptyMetricDimensionsHash) }},
		{"non hexadecimal hash", valid, func(v *ports.MetricValue) { v.DimensionHash = strings.Repeat("z", 64) }},
		{"empty dimensions hash mismatch", valid, func(v *ports.MetricValue) { v.DimensionHash = strings.Repeat("a", 64) }},
		{"invalid dimensions json", withDimensions, func(v *ports.MetricValue) { v.Dimensions = []byte(`{"region":`) }},
		{"trailing dimensions json", withDimensions, func(v *ports.MetricValue) { v.Dimensions = []byte(`{} {}`) }},
		{"dimensions hash mismatch", withDimensions, func(v *ports.MetricValue) { v.DimensionHash = strings.Repeat("a", 64) }},
		{"oversized dimensions", withDimensions, func(v *ports.MetricValue) {
			v.Dimensions = []byte(`"` + strings.Repeat("a", maxMetricDimensionsBytes) + `"`)
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value := test.base
			value.Dimensions = append([]byte(nil), test.base.Dimensions...)
			test.mutate(&value)
			if err := validateMetricUpsert(value); err == nil {
				t.Fatal("invalid metric was accepted")
			}
		})
	}
}

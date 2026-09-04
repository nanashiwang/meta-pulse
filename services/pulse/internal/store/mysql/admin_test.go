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

func TestContentCandidateCreateValidatesPendingSourceProjection(t *testing.T) {
	now := time.Now()
	valid := ports.ContentCandidate{
		SourceSystem: "answer", SourceContentID: "42", ContentType: "question",
		AuthorUserID: 7, Title: "如何接入", SourceCreatedAt: now,
		PayloadHash: strings.Repeat("a", 64), CursorValue: "42",
		Status: ports.ContentCandidatePending, CreatedAt: now,
	}
	if err := validateContentCandidateCreate(valid); err != nil {
		t.Fatalf("valid content candidate rejected: %v", err)
	}
	withoutTitle := valid
	withoutTitle.Title = ""
	if err := validateContentCandidateCreate(withoutTitle); err != nil {
		t.Fatalf("candidate without title rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*ports.ContentCandidate)
	}{
		{"explicit id", func(v *ports.ContentCandidate) { v.ID = 1 }},
		{"missing author", func(v *ports.ContentCandidate) { v.AuthorUserID = 0 }},
		{"padded source", func(v *ports.ContentCandidate) { v.SourceSystem = " answer" }},
		{"long source id", func(v *ports.ContentCandidate) { v.SourceContentID = strings.Repeat("内", 192) }},
		{"long content type", func(v *ports.ContentCandidate) { v.ContentType = strings.Repeat("类", 65) }},
		{"long title", func(v *ports.ContentCandidate) { v.Title = strings.Repeat("题", 501) }},
		{"malformed title", func(v *ports.ContentCandidate) { v.Title = string([]byte{0xff}) }},
		{"short payload hash", func(v *ports.ContentCandidate) { v.PayloadHash = "abcd" }},
		{"uppercase payload hash", func(v *ports.ContentCandidate) { v.PayloadHash = strings.Repeat("A", 64) }},
		{"invalid cursor", func(v *ports.ContentCandidate) { v.CursorValue = " " }},
		{"terminal status", func(v *ports.ContentCandidate) { v.Status = ports.ContentCandidateApproved }},
		{"review actor on create", func(v *ports.ContentCandidate) { v.ReviewActorID = "operator" }},
		{"review time on create", func(v *ports.ContentCandidate) { v.ReviewedAt = &now }},
		{"zero source time", func(v *ports.ContentCandidate) { v.SourceCreatedAt = time.Time{} }},
		{"zero creation time", func(v *ports.ContentCandidate) { v.CreatedAt = time.Time{} }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.mutate(&value)
			if err := validateContentCandidateCreate(value); err == nil {
				t.Fatal("invalid content candidate was accepted")
			}
		})
	}
}

func TestContentCandidateReviewOnlyAcceptsPendingToTerminalInput(t *testing.T) {
	repo := &contentRepository{}
	now := time.Now()
	cases := []struct {
		candidateID uint64
		status      string
		actorType   string
		actorID     string
		reason      string
		reviewedAt  time.Time
	}{
		{0, ports.ContentCandidateApproved, "admin", "operator", "good", now},
		{1, ports.ContentCandidatePending, "admin", "operator", "good", now},
		{1, "unknown", "admin", "operator", "good", now},
		{1, ports.ContentCandidateApproved, " admin", "operator", "good", now},
		{1, ports.ContentCandidateApproved, "admin", "operator", " ", now},
		{1, ports.ContentCandidateApproved, "admin", "operator", "good", time.Time{}},
	}
	for _, test := range cases {
		if err := repo.ReviewCandidate(context.Background(), test.candidateID, test.status, test.actorType, test.actorID, test.reason, test.reviewedAt); err == nil {
			t.Fatalf("invalid review accepted: %+v", test)
		}
	}
}

func TestContentCandidateQueriesValidateIdentityBeforeDatabase(t *testing.T) {
	repo := &contentRepository{}
	if _, err := repo.FindCandidateBySource(context.Background(), "", "42"); err == nil {
		t.Fatal("empty candidate source accepted")
	}
	if _, err := repo.FindCandidateBySource(context.Background(), "answer", strings.Repeat("x", 192)); err == nil {
		t.Fatal("oversized candidate source id accepted")
	}
	if _, err := repo.FindCandidateForUpdate(context.Background(), 0); err == nil {
		t.Fatal("zero candidate id accepted")
	}
}

func TestContentAwardCreateValidatesBudgetGrantAndInitialStatus(t *testing.T) {
	now := time.Now()
	valid := ports.ContentAward{
		CandidateID: 1, AwardVersion: 1, ActionID: "content_award:question:42:1",
		PeriodID: 2, UserID: 3, Amount: 10, RewardType: "quota",
		BudgetType: contentRewardBudgetType, GrantID: "pg_content_1",
		Status: ports.ContentAwardPending, Reason: "quality content", CreatedAt: now,
	}
	if err := validateContentAwardCreate(valid); err != nil {
		t.Fatalf("valid content award rejected: %v", err)
	}
	for _, status := range []string{ports.ContentAwardIneligible, ports.ContentAwardLimited} {
		withoutGrant := valid
		withoutGrant.Status = status
		withoutGrant.GrantID = ""
		if err := validateContentAwardCreate(withoutGrant); err != nil {
			t.Fatalf("valid %s award rejected: %v", status, err)
		}
	}
	persisted := valid
	persisted.ID = 9
	persisted.Status = ports.ContentAwardSettled
	if err := validatePersistedContentAward(persisted); err != nil {
		t.Fatalf("valid persisted award rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*ports.ContentAward)
	}{
		{"explicit id", func(v *ports.ContentAward) { v.ID = 1 }},
		{"missing candidate", func(v *ports.ContentAward) { v.CandidateID = 0 }},
		{"missing version", func(v *ports.ContentAward) { v.AwardVersion = 0 }},
		{"padded action", func(v *ports.ContentAward) { v.ActionID = " " + v.ActionID }},
		{"long action", func(v *ports.ContentAward) { v.ActionID = strings.Repeat("a", 192) }},
		{"missing period", func(v *ports.ContentAward) { v.PeriodID = 0 }},
		{"missing user", func(v *ports.ContentAward) { v.UserID = 0 }},
		{"zero amount", func(v *ports.ContentAward) { v.Amount = 0 }},
		{"negative amount", func(v *ports.ContentAward) { v.Amount = -1 }},
		{"long reward type", func(v *ports.ContentAward) { v.RewardType = strings.Repeat("r", 65) }},
		{"wrong budget", func(v *ports.ContentAward) { v.BudgetType = "loyalty" }},
		{"pending without grant", func(v *ports.ContentAward) { v.GrantID = "" }},
		{"long grant", func(v *ports.ContentAward) { v.GrantID = strings.Repeat("g", 65) }},
		{"settled on create", func(v *ports.ContentAward) { v.Status = ports.ContentAwardSettled }},
		{"unknown status", func(v *ports.ContentAward) { v.Status = "unknown" }},
		{"long reason", func(v *ports.ContentAward) { v.Reason = strings.Repeat("理", 501) }},
		{"zero creation time", func(v *ports.ContentAward) { v.CreatedAt = time.Time{} }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.mutate(&value)
			if err := validateContentAwardCreate(value); err == nil {
				t.Fatal("invalid content award was accepted")
			}
		})
	}

	ineligibleWithGrant := valid
	ineligibleWithGrant.ID = 1
	ineligibleWithGrant.Status = ports.ContentAwardIneligible
	if err := validatePersistedContentAward(ineligibleWithGrant); err == nil {
		t.Fatal("persisted ineligible award with grant was accepted")
	}
}

func TestContentAwardQueriesAndLimitsValidateIdentityBeforeDatabase(t *testing.T) {
	repo := &contentRepository{}
	if _, err := repo.FindAwardByAction(context.Background(), " "); err == nil {
		t.Fatal("blank content award action accepted")
	}
	if _, err := repo.FindAwardByActionForUpdate(context.Background(), strings.Repeat("a", 192)); err == nil {
		t.Fatal("oversized content award action accepted")
	}
	if err := repo.MarkAwardSettledByGrantID(context.Background(), " "); err == nil {
		t.Fatal("blank grant id accepted")
	}
	if _, err := repo.SumUserActiveAwards(context.Background(), 0, 1); err == nil {
		t.Fatal("zero user award scope accepted")
	}
	if _, err := repo.SumUserActiveAwards(context.Background(), 1, 0); err == nil {
		t.Fatal("zero period award scope accepted")
	}
	if _, err := repo.SumDailyActiveAwards(context.Background(), time.Time{}); err == nil {
		t.Fatal("zero daily award scope accepted")
	}
}

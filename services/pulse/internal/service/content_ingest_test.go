package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type staticContentSource struct {
	events  []ports.ContentEvent
	onFetch func()
}

func (s staticContentSource) Fetch(context.Context, string, int) ([]ports.ContentEvent, error) {
	if s.onFetch != nil {
		s.onFetch()
	}
	return append([]ports.ContentEvent(nil), s.events...), nil
}

type contentIngestUnit struct {
	store   *memoryLedgerStore
	content *memoryContentStore
}

func (u contentIngestUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{Cursor: memoryCursorRepo{u.store}, Content: u.content, Conflict: memoryConflictRepo{u.store}, Period: memoryPeriodRepo{u.store}})
}

func TestContentIngestIsReadOnlyAndIdempotent(t *testing.T) {
	created := time.Unix(1700000000, 0).UTC()
	event := ports.ContentEvent{SourceContentID: "42", ContentType: "question", AuthorUserID: 9, Title: "如何接入", SourceCreatedAt: created, CursorValue: "42", PayloadHash: "hash-1"}
	store := newMemoryLedgerStore()
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, StartsAt: created.Add(-time.Hour), EndsAt: created.Add(time.Hour)}}
	content := &memoryContentStore{}
	s, err := NewContentIngestService(contentIngestUnit{store: store, content: content}, staticContentSource{events: []ports.ContentEvent{event}}, ContentIngestConfig{BatchSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.IngestBatch(context.Background())
	if err != nil || first.Accepted != 1 || len(content.candidates) != 1 || store.cursor.Value != "42" {
		t.Fatalf("first=%+v err=%v candidates=%d cursor=%+v", first, err, len(content.candidates), store.cursor)
	}
	second, err := s.IngestBatch(context.Background())
	if err != nil || second.Replayed != 1 || len(content.candidates) != 1 {
		t.Fatalf("second=%+v err=%v candidates=%d", second, err, len(content.candidates))
	}
	changed := event
	changed.PayloadHash = "hash-2"
	s.source = staticContentSource{events: []ports.ContentEvent{changed}}
	third, err := s.IngestBatch(context.Background())
	if err != nil || third.Conflicts != 1 || len(store.conflicts) != 1 || len(store.entries) != 0 {
		t.Fatalf("third=%+v err=%v conflicts=%d ledger=%d", third, err, len(store.conflicts), len(store.entries))
	}
}

func TestContentIngestCanStageBeforeFirstActivePeriod(t *testing.T) {
	created := time.Unix(1500000000, 0).UTC()
	event := ports.ContentEvent{SourceContentID: "1", ContentType: "question", AuthorUserID: 9, SourceCreatedAt: created, CursorValue: "1", PayloadHash: "hash"}
	store := newMemoryLedgerStore()
	content := &memoryContentStore{}
	s, err := NewContentIngestService(contentIngestUnit{store: store, content: content}, staticContentSource{events: []ports.ContentEvent{event}}, ContentIngestConfig{BatchSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.IngestBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if content.candidates[1].PeriodID != 0 || store.cursor.Value != "1" {
		t.Fatalf("candidate=%+v cursor=%+v", content.candidates[1], store.cursor)
	}
}

func TestContentIngestRejectsOversizedMetadata(t *testing.T) {
	created := time.Unix(1700000000, 0).UTC()
	event := ports.ContentEvent{SourceContentID: "42", ContentType: "question", AuthorUserID: 9, Title: strings.Repeat("标题", 251), SourceCreatedAt: created, CursorValue: "42", PayloadHash: "hash-1"}
	store := newMemoryLedgerStore()
	content := &memoryContentStore{}
	s, err := NewContentIngestService(contentIngestUnit{store: store, content: content}, staticContentSource{events: []ports.ContentEvent{event}}, ContentIngestConfig{BatchSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.IngestBatch(context.Background()); err == nil {
		t.Fatal("oversized content metadata was accepted")
	}
	if len(content.candidates) != 0 || store.cursor.Value != "" {
		t.Fatalf("oversized content mutated state candidates=%d cursor=%+v", len(content.candidates), store.cursor)
	}
}

func TestContentIngestStaleBatchDoesNotRegressCursor(t *testing.T) {
	created := time.Unix(1700000000, 0).UTC()
	stale := ports.ContentEvent{SourceContentID: "1", ContentType: "question", AuthorUserID: 9, SourceCreatedAt: created, CursorValue: "1", PayloadHash: "hash-1"}
	store := newMemoryLedgerStore()
	content := &memoryContentStore{}
	source := staticContentSource{
		events: []ports.ContentEvent{stale},
		onFetch: func() {
			store.cursor.Value = "2"
			store.cursor.Version++
		},
	}
	s, err := NewContentIngestService(contentIngestUnit{store: store, content: content}, source, ContentIngestConfig{BatchSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	report, err := s.IngestBatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Fetched != 1 || report.Accepted != 0 || len(content.candidates) != 0 {
		t.Fatalf("report=%+v candidates=%d", report, len(content.candidates))
	}
	if store.cursor.Value != "2" {
		t.Fatalf("content cursor regressed to %q", store.cursor.Value)
	}
}

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type cursorMemory struct {
	cursor ports.Cursor
	saved  []ports.Cursor
}

func (m *cursorMemory) GetOrCreateForUpdate(_ context.Context, name, sourceSystem string) (ports.Cursor, error) {
	if m.cursor.Name == "" {
		m.cursor.Name = name
		m.cursor.SourceSystem = sourceSystem
	}
	return m.cursor, nil
}

func (m *cursorMemory) Save(_ context.Context, cursor ports.Cursor) error {
	m.cursor = cursor
	m.saved = append(m.saved, cursor)
	return nil
}

type cursorSeekUnit struct {
	cursor *cursorMemory
	audit  *auditMemory
}

func (u *cursorSeekUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{Cursor: u.cursor, Audit: u.audit})
}

func newCursorSeekUnit(value string) *cursorSeekUnit {
	return &cursorSeekUnit{cursor: &cursorMemory{cursor: ports.Cursor{Value: value}}, audit: &auditMemory{}}
}

func validSeekCommand() CursorSeekCommand {
	return CursorSeekCommand{
		ActorType: "operator", ActorID: "ops-1",
		SkipBefore: time.Unix(1789000000, 0),
		Reason:     "丢弃积压，从新周期起算",
	}
}

func TestSeekAdvancesCursorAndWatermark(t *testing.T) {
	unit := newCursorSeekUnit("1788623419:9250543")
	svc, err := NewCursorSeekService(unit, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Seek(context.Background(), validSeekCommand())
	if err != nil {
		t.Fatal(err)
	}
	if result.Before != "1788623419:9250543" {
		t.Fatalf("before = %s", result.Before)
	}
	// The target sits one second before SkipBefore at the maximum id, so the
	// first row of SkipBefore itself is still ingested.
	at, id, ok := parseLogCursor(result.After)
	if !ok || at != 1789000000-1 || id != int64(^uint64(0)>>1) {
		t.Fatalf("after = %s", result.After)
	}
	if result.WatermarkAt == nil || !result.WatermarkAt.Equal(time.Unix(1789000000-1, 0)) {
		t.Fatalf("watermark = %v", result.WatermarkAt)
	}
	if unit.cursor.cursor.Version != 1 {
		t.Fatalf("version = %d, want 1", unit.cursor.cursor.Version)
	}
}

// Invariant #16: a manual jump that forfeits rows must leave an audit trail.
func TestSeekWritesAuditLog(t *testing.T) {
	unit := newCursorSeekUnit("1788623419:9250543")
	svc, _ := NewCursorSeekService(unit, time.Now)
	if _, err := svc.Seek(context.Background(), validSeekCommand()); err != nil {
		t.Fatal(err)
	}
	if len(unit.audit.logs) != 1 {
		t.Fatalf("audit logs = %d, want 1", len(unit.audit.logs))
	}
	log := unit.audit.logs[0]
	if log.Action != "cursor_seek" || log.ResourceType != "worker_cursor" {
		t.Fatalf("audit log = %+v", log)
	}
	if log.Reason == "" || log.ResourceID != DefaultUsageCursorName {
		t.Fatalf("audit log = %+v", log)
	}
	if !strings.Contains(string(log.AfterJSON), "1788623419:9250543") {
		t.Fatalf("audit payload does not record the previous position: %s", log.AfterJSON)
	}
}

// Rewinding would replay rows into an already-closed period's watermark.
func TestSeekRefusesToMoveBackwards(t *testing.T) {
	unit := newCursorSeekUnit("1790000000:1")
	svc, _ := NewCursorSeekService(unit, time.Now)
	_, err := svc.Seek(context.Background(), validSeekCommand())
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("error = %v, want a conflict", err)
	}
	if len(unit.cursor.saved) != 0 || len(unit.audit.logs) != 0 {
		t.Fatal("a rejected seek still wrote state")
	}
}

// An empty cursor is the beginning of time, which every valid target is ahead of.
func TestSeekAcceptsEmptyCursor(t *testing.T) {
	unit := newCursorSeekUnit("")
	svc, _ := NewCursorSeekService(unit, time.Now)
	if _, err := svc.Seek(context.Background(), validSeekCommand()); err != nil {
		t.Fatalf("empty cursor rejected: %v", err)
	}
}

func TestSeekRequiresActorReasonAndTarget(t *testing.T) {
	for name, mutate := range map[string]func(*CursorSeekCommand){
		"no actor id": func(c *CursorSeekCommand) { c.ActorID = " " },
		"no reason":   func(c *CursorSeekCommand) { c.Reason = "" },
		"zero time":   func(c *CursorSeekCommand) { c.SkipBefore = time.Time{} },
	} {
		unit := newCursorSeekUnit("")
		svc, _ := NewCursorSeekService(unit, time.Now)
		command := validSeekCommand()
		mutate(&command)
		if _, err := svc.Seek(context.Background(), command); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

// String comparison would order "999:1" after "1000:1" and silently allow a
// rewind across a digit-count boundary.
func TestLogCursorLessComparesNumerically(t *testing.T) {
	cases := []struct {
		left, right string
		want        bool
	}{
		{"999:1", "1000:1", true},
		{"1000:1", "999:1", false},
		{"1000:2", "1000:10", true},
		{"1000:10", "1000:2", false},
		{"1000:5", "1000:5", false},
		{"", "1000:5", true},
		{"garbage", "1000:5", true},
		{"1000:5", "garbage", false},
	}
	for _, c := range cases {
		if got := logCursorLess(c.left, c.right); got != c.want {
			t.Fatalf("logCursorLess(%q, %q) = %v, want %v", c.left, c.right, got, c.want)
		}
	}
}

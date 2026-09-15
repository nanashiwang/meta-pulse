package newapi

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func logFetchQuery() string {
	return regexp.QuoteMeta(`
SELECT /*+ MAX_EXECUTION_TIME(` + strconv.Itoa(logFetchStatementTimeoutMillis) + `) */ ` + logFetchColumns + `
FROM logs
WHERE type = ?
  AND created_at >= ?
  AND (created_at > ? OR (created_at = ? AND id > ?))
ORDER BY created_at ASC, id ASC
LIMIT ?`)
}

func logRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "user_id", "created_at", "type", "model_name", "quota", "channel_id", "request_id", "other"})
}

// A single-valued type predicate is what lets new-api's (created_at, type)
// index serve the cursor order. An IN list would fall back to a full scan plus
// filesort over the whole logs table, so the per-type shape is load-bearing.
func TestFetchQueriesEachTypeSeparatelyWithSargableCursor(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader, err := NewLogReaderForDB(db)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery(logFetchQuery()).
		WithArgs(LogTypeConsume, int64(1700000000), int64(1700000000), int64(1700000000), int64(42), 500).
		WillReturnRows(logRows().AddRow(43, 7, 1700000001, LogTypeConsume, "gpt-4", 120, 3, "req-a", ""))
	mock.ExpectQuery(logFetchQuery()).
		WithArgs(LogTypeRefund, int64(1700000000), int64(1700000000), int64(1700000000), int64(42), 500).
		WillReturnRows(logRows().AddRow(44, 7, 1700000002, LogTypeRefund, "gpt-4", -120, 3, "req-b", ""))

	records, err := reader.Fetch(context.Background(), Cursor{CreatedAt: 1700000000, ID: 42}, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// The merged page must be in the same (created_at, id) order the single
// statement produced, because the cursor is derived from the last row.
func TestFetchMergesTypePagesInCursorOrder(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader, err := NewLogReaderForDB(db)
	if err != nil {
		t.Fatal(err)
	}

	// Refund rows interleave with consume rows, including a created_at tie
	// that must be broken by id.
	mock.ExpectQuery(logFetchQuery()).
		WithArgs(LogTypeConsume, int64(0), int64(0), int64(0), int64(0), 10).
		WillReturnRows(logRows().
			AddRow(10, 1, 1700000000, LogTypeConsume, "m", 1, 1, "", "").
			AddRow(30, 1, 1700000005, LogTypeConsume, "m", 1, 1, "", ""))
	mock.ExpectQuery(logFetchQuery()).
		WithArgs(LogTypeRefund, int64(0), int64(0), int64(0), int64(0), 10).
		WillReturnRows(logRows().
			AddRow(20, 1, 1700000000, LogTypeRefund, "m", -1, 1, "", "").
			AddRow(25, 1, 1700000003, LogTypeRefund, "m", -1, 1, "", ""))

	records, err := reader.Fetch(context.Background(), Cursor{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []int64{10, 20, 25, 30}
	if len(records) != len(wantIDs) {
		t.Fatalf("got %d records, want %d", len(records), len(wantIDs))
	}
	for i, want := range wantIDs {
		if records[i].ID != want {
			t.Fatalf("records[%d].ID = %d, want %d", i, records[i].ID, want)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Each type is queried for up to limit rows, so the merge can exceed the
// caller's batch size and must be truncated. Dropping the tail is safe: the
// cursor advances only past rows actually returned.
func TestFetchTruncatesMergedPageToLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader, err := NewLogReaderForDB(db)
	if err != nil {
		t.Fatal(err)
	}

	consume := logRows()
	refund := logRows()
	for i := 0; i < 3; i++ {
		consume.AddRow(int64(100+i), 1, int64(1700000000+i*2), LogTypeConsume, "m", 1, 1, "", "")
		refund.AddRow(int64(200+i), 1, int64(1700000001+i*2), LogTypeRefund, "m", -1, 1, "", "")
	}
	mock.ExpectQuery(logFetchQuery()).WithArgs(LogTypeConsume, int64(0), int64(0), int64(0), int64(0), 3).WillReturnRows(consume)
	mock.ExpectQuery(logFetchQuery()).WithArgs(LogTypeRefund, int64(0), int64(0), int64(0), int64(0), 3).WillReturnRows(refund)

	records, err := reader.Fetch(context.Background(), Cursor{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("got %d records, want 3", len(records))
	}
	// The retained rows are the globally earliest ones, not one type's page.
	wantIDs := []int64{100, 200, 101}
	for i, want := range wantIDs {
		if records[i].ID != want {
			t.Fatalf("records[%d].ID = %d, want %d", i, records[i].ID, want)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A failing type page must fail the whole batch rather than silently return a
// partial page, which would advance the cursor past unread rows.
func TestFetchFailsWhenATypePageFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader, err := NewLogReaderForDB(db)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery(logFetchQuery()).
		WithArgs(LogTypeConsume, int64(0), int64(0), int64(0), int64(0), 5).
		WillReturnRows(logRows().AddRow(1, 1, 1700000000, LogTypeConsume, "m", 1, 1, "", ""))
	mock.ExpectQuery(logFetchQuery()).
		WithArgs(LogTypeRefund, int64(0), int64(0), int64(0), int64(0), 5).
		WillReturnError(context.DeadlineExceeded)

	if _, err := reader.Fetch(context.Background(), Cursor{}, 5); err == nil {
		t.Fatal("partial page was returned as success")
	}
}

func TestFetchRejectsInvalidBatchSize(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader, err := NewLogReaderForDB(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, -1, 5001} {
		if _, err := reader.Fetch(context.Background(), Cursor{}, limit); err == nil {
			t.Fatalf("limit %d accepted", limit)
		}
	}
}

// The statement-level cap is the inner bound that keeps a bad plan from
// holding a LOG_DB thread for the whole job timeout.
func TestFetchCarriesStatementTimeoutHint(t *testing.T) {
	if !strings.Contains(logFetchQuery(), "MAX_EXECUTION_TIME") {
		t.Fatal("statement timeout hint is missing from the fetch query")
	}
}

// logFetchQuery is regexp-quoted, so match on the operator only.
// Without the redundant lower bound the cursor predicate is not sargable and
// MySQL scans the index from the oldest row on every page, which is the shape
// that stalled ingestion in production.
func TestFetchCarriesRangeLowerBound(t *testing.T) {
	if !strings.Contains(logFetchQuery(), "created_at >=") {
		t.Fatal("range lower bound is missing from the fetch query")
	}
}

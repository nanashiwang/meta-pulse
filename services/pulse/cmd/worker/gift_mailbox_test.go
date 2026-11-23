package main

import (
	"context"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"os"
	"path/filepath"
	"testing"
)

type capturedGift struct {
	n    int
	user uint64
	v    ports.GiftOrder
}

func (s *capturedGift) QueueGift(_ context.Context, user uint64, v ports.GiftOrder) error {
	s.n++
	s.user = user
	s.v = v
	return nil
}
func TestGiftMailboxAcceptsOnlyCompletedMatchingReceipts(t *testing.T) {
	d := t.TempDir()
	b := []byte(`{"round":7,"actor":1,"user":42,"forum":2,"flash":15,"tickets":3}`)
	for _, name := range []string{".gift-temp", "7-43.json", "7-42.json"} {
		if e := os.WriteFile(filepath.Join(d, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	cursor := ""
	s := &capturedGift{}
	if e := importGifts(context.Background(), d, &cursor, s); e == nil || s.n != 1 || s.user != 42 || s.v.Flash != 15 || s.v.Tickets != 3 {
		t.Fatal(e, s)
	}
	if e := os.Remove(filepath.Join(d, "7-43.json")); e != nil {
		t.Fatal(e)
	}
	if e := importGifts(context.Background(), d, &cursor, s); e != nil || s.n != 2 {
		t.Fatal(e, s)
	}
}

func TestAwardReceiptKeepsExactIDThroughSharedDelivery(t *testing.T) {
	dir := t.TempDir()
	// Above JavaScript's safe integer range; the server-side mailbox must not round it.
	const id = 4611686018427387905
	body := `{"round":4611686018427387905,"actor":1,"user":42,"forum":2,"flash":5000,"tickets":0}`
	if err := os.WriteFile(filepath.Join(dir, "4611686018427387905-42.json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	capture := &capturedGift{}
	cursor := ""
	if err := importGifts(context.Background(), dir, &cursor, capture); err != nil || capture.n != 1 || capture.v.Round != id || capture.v.Flash != 5000 {
		t.Fatal(err, capture)
	}
}

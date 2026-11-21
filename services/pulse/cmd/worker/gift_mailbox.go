package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type giftInbox interface {
	QueueGift(context.Context, uint64, ports.GiftOrder) error
}

// Receipts enter through a read-only, non-web volume; no HTTP handler can
// supply a prize or recipient. The Pulse transaction rejects changed replays.
func importGifts(ctx context.Context, dir string, last *string, g giftInbox) error {
	entries, e := os.ReadDir(dir)
	if e != nil {
		return e
	}
	i := sort.Search(len(entries), func(i int) bool { return entries[i].Name() > *last })
	var first error
	limit := i + 100
	for ; i < len(entries) && i < limit; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		f := entries[i]
		*last = f.Name()
		if !strings.HasSuffix(f.Name(), ".json") || !f.Type().IsRegular() {
			continue
		}
		if e := importGift(ctx, dir, f.Name(), g); e != nil {
			first = e
		}
	}
	if i == len(entries) {
		*last = ""
	}
	return first
}
func importGift(ctx context.Context, dir, name string, g giftInbox) error {
	f, e := os.Open(filepath.Join(dir, name))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 1025))
	if e != nil || len(b) > 1024 {
		return errors.New("invalid gift receipt")
	}
	var v struct {
		ports.GiftOrder
		User uint64 `json:"user"`
	}
	if json.Unmarshal(b, &v) != nil || v.User == 0 || fmt.Sprintf("%d-%d.json", v.Round, v.User) != name {
		return errors.New("invalid gift receipt")
	}
	return g.QueueGift(ctx, v.User, v.GiftOrder)
}

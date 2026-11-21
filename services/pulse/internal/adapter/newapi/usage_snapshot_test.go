package newapi

import (
	"context"
	"fmt"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecordedGiftUsage(t *testing.T) {
	for _, tc := range []struct {
		json           string
		status         int
		valid, missing bool
	}{{"null", 200, true, true}, {"0", 200, true, false}, {"15001", 200, true, false}, {"1.5", 200, false, false}, {"-1", 200, false, false}, {"0", 503, false, false}} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" || r.URL.RawQuery != "" || r.Header.Get(security.HeaderUserID) != "42" {
				t.Error("invalid read contract")
			}
			b, _ := io.ReadAll(r.Body)
			if len(b) != 0 {
				t.Error("unexpected body")
			}
			w.WriteHeader(tc.status)
			fmt.Fprintf(w, `{"date":"2026-11-21","completeness":"unknown","recorded_consume_quota":%s}`, tc.json)
		}))
		c, _ := NewBenefitClient(s.URL, []byte(strings.Repeat("t", 32)), nil)
		q, e := c.RecordedUsage(context.Background(), 42, "2026-11-21")
		if (e == nil) != tc.valid || tc.valid && (q == nil) != tc.missing {
			t.Fatal(q, e)
		}
		if _, e = c.RecordedUsage(context.Background(), 42, "2026-11-20"); e == nil {
			t.Fatal("stale day")
		}
		s.Close()
	}
}

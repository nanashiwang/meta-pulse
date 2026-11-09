package llmstats

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/catalog"
)

var now = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func fixtureServer(t *testing.T, status map[string]int) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("Authorization") != "Bearer sk_ze_test" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"code":"invalid_api_key","message":"bad key"}}`))
			return
		}
		if code := status[r.URL.Path]; code != 0 {
			delete(status, r.URL.Path)
			if code == 429 {
				w.Header().Set("Retry-After", "1")
			}
			w.WriteHeader(code)
			w.Write([]byte(`{"error":{"code":"rate_limit_exceeded","message":"slow down"}}`))
			return
		}
		file := ""
		switch r.URL.Path {
		case "/stats/v1/models":
			if r.URL.Query().Get("limit") != "200" {
				t.Errorf("models limit %q", r.URL.Query().Get("limit"))
			}
			file = "models-page1.json"
			if r.URL.Query().Get("cursor") == "c2" {
				file = "models-page2.json"
			}
		case "/stats/v1/benchmarks":
			file = "benchmarks.json"
		case "/stats/v1/scores":
			file = "scores.json"
		default:
			w.WriteHeader(404)
			return
		}
		data, err := os.ReadFile("testdata/" + file)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func fetch(t *testing.T, status map[string]int) (Dataset, *Client) {
	t.Helper()
	srv, _ := fixtureServer(t, status)
	c := NewClient(srv.URL+"/stats", "sk_ze_test")
	c.Pause = 0
	ds, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ds, c
}

func TestFetchPaginatesAndRetries429(t *testing.T) {
	ds, c := fetch(t, map[string]int{"/stats/v1/benchmarks": 429})
	if len(ds.Models) != 3 || len(ds.Benchmarks) != 4 || len(ds.Scores) != 7 {
		t.Fatalf("got %d models %d benchmarks %d scores", len(ds.Models), len(ds.Benchmarks), len(ds.Scores))
	}
	if c.Requests != 5 {
		t.Fatalf("requests %d, want 2 model pages + 429 retry + benchmarks + scores", c.Requests)
	}
}

func TestFetchErrorsDoNotLeakKey(t *testing.T) {
	srv, _ := fixtureServer(t, map[string]int{})
	c := NewClient(srv.URL+"/stats", "sk_ze_wrong")
	c.Pause = 0
	_, err := c.Fetch(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 401 || apiErr.Code != "invalid_api_key" {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), "sk_ze_wrong") {
		t.Fatal("API key leaked into error")
	}
	if _, err := NewClient(srv.URL, "").Fetch(context.Background()); err == nil {
		t.Fatal("missing key accepted")
	}
}

func TestConvertProducesAValidDocument(t *testing.T) {
	ds, _ := fetch(t, map[string]int{})
	doc, rep := Convert(ds, now, nil)
	b, err := catalog.Validate(doc, catalog.Known{})
	if err != nil {
		t.Fatalf("converted document must validate: %v", err)
	}
	if rep.Models != 3 || rep.SkippedScores != 2 {
		t.Fatalf("report %s", rep)
	}
	models := map[string]catalog.Model{}
	for _, m := range b.Models {
		models[m.Slug] = m
	}
	alpha := models["example-lab-alpha-pro-2"]
	if alpha.Provider != "example-lab" || alpha.ContextTokens != 1_000_000 || alpha.License != "proprietary" || alpha.Status != "available" {
		t.Fatalf("alpha %+v", alpha)
	}
	if models["example-lab-next"].Status != "unreleased" {
		t.Fatal("future release must be unreleased")
	}
	if open := models["open-org-open-7b"]; !open.OpenWeights || open.License != "Apache 2.0" || open.SourceURL == "" {
		t.Fatalf("open %+v", open)
	}
	dims := map[string]string{}
	for _, x := range b.Benchmarks {
		dims[x.Slug] = string(x.Dimension)
	}
	if dims["raw-gpqa-diamond"] != "reasoning" || dims["raw-swe-verified"] != "coding" || dims["raw-poem-arena"] != "other" {
		t.Fatalf("dimensions %v", dims)
	}
	if _, ok := dims["raw-tau-retail"]; ok {
		t.Fatal("benchmark with only a future score must be left out")
	}
	if strings.Join(rep.UnmappedCategories, ",") != "creative-writing" {
		t.Fatalf("unmapped %v", rep.UnmappedCategories)
	}
	values := map[string]int64{}
	self := map[string]string{}
	for _, r := range b.Results {
		values[r.Model+"/"+r.Benchmark] = r.Value
		self[r.Model+"/"+r.Benchmark] = r.Settings
	}
	if values["example-lab-alpha-pro-2/raw-gpqa-diamond"] != 87305 || values["example-lab-alpha-pro-2/raw-swe-verified"] != 615 {
		t.Fatalf("raw score preservation %v", values)
	}
	if self["example-lab-alpha-pro-2/raw-gpqa-diamond"] != "self-reported" {
		t.Fatal("self-reported flag lost")
	}
	prices := map[string][2]int64{}
	for _, p := range b.Prices {
		prices[p.Offering] = [2]int64{p.InputMicro, p.OutputMicro}
	}
	if prices["example-lab-alpha-pro-2-example-lab"] != [2]int64{3_000_000, 15_000_000} || prices["example-lab-alpha-pro-2-example-host"] != [2]int64{2_500_000, 12_000_000} {
		t.Fatalf("prices %v", prices)
	}
	if len(b.Speeds) != 2 || b.Speeds[0].TTFTMillis == 0 {
		t.Fatalf("speeds %+v", b.Speeds)
	}
}

func TestUnchangedPricesAreNotRepeated(t *testing.T) {
	ds, _ := fetch(t, map[string]int{})
	latest := map[string]PriceKey{"example-lab-alpha-pro-2-example-lab": {3_000_000, 15_000_000}}
	doc, rep := Convert(ds, now, latest)
	for _, p := range doc.Prices {
		if p.Offering == "example-lab-alpha-pro-2-example-lab" {
			t.Fatal("unchanged price re-recorded")
		}
	}
	if rep.UnchangedPrices != 1 {
		t.Fatalf("report %s", rep)
	}
	again, _ := Convert(ds, now.Add(2*time.Hour), nil)
	b1, _ := catalog.Validate(doc, catalog.Known{})
	b2, _ := catalog.Validate(again, catalog.Known{})
	if b1.Results[0].Fingerprint != b2.Results[0].Fingerprint || b1.Speeds[0].Fingerprint != b2.Speeds[0].Fingerprint {
		t.Fatal("same-day resync must produce identical fingerprints")
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{"anthropic/claude-opus-4.5": "anthropic-claude-opus-4.5", "  GPT 5 (Turbo)  ": "gpt-5-turbo", "a..b--c": "a-b-c"}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
	long := Slug(strings.Repeat("x", 200))
	if len(long) > 80 || !slugCheck.MatchString(long) || long == Slug(strings.Repeat("x", 201)) {
		t.Fatalf("long slug %q", long)
	}
	if s := Slug("！！"); !slugCheck.MatchString(s) {
		t.Fatalf("empty slug %q", s)
	}
}

func TestTopScoresAreImportedWithoutScalingAgainstPeers(t *testing.T) {
	ds, _ := fetch(t, map[string]int{})
	ds.Models[0].TopScores["overall"] = "59.9"
	doc, _ := Convert(ds, now, nil)
	b, err := catalog.Validate(doc, catalog.Known{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range b.Results {
		if r.Benchmark == "llmstats-rating-overall" {
			found = true
			if r.Value != 59900 {
				t.Fatal(r.Value)
			}
		}
	}
	if !found {
		t.Fatal("source overall score missing")
	}
}

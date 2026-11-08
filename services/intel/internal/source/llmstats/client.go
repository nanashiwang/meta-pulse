// Package llmstats pulls models, benchmarks and scores from the LLM Stats data
// API (https://docs.llm-stats.com) and converts them into an Intel import
// document. Every page built from this data must credit "Data by LLM Stats"
// with a link to llm-stats.com; the frontend does so for source "llm-stats".
package llmstats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.llm-stats.com/stats"
	maxBody        = 32 << 20
	maxPages       = 200
)

// Client is a small, polite API client: sequential requests, a pause between
// pages to stay under the per-minute burst limit, and one retry on 429.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	// Pause between requests; the documented burst limit is 60/min.
	Pause time.Duration
	// Requests counts calls made, so a sync can report quota use.
	Requests int
}

func NewClient(baseURL, apiKey string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, HTTP: &http.Client{Timeout: 45 * time.Second}, Pause: 1100 * time.Millisecond}
}

// APIError carries the documented error envelope code, never the key.
type APIError struct {
	Status int
	Code   string
	Msg    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("llm-stats API %d %s: %s", e.Status, e.Code, e.Msg)
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	if c.APIKey == "" {
		return errors.New("llm-stats API key is not configured")
	}
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	for attempt := 0; attempt < 2; attempt++ {
		if c.Requests > 0 && c.Pause > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.Pause):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "metar-intel/1 (+https://metarhub.com/models)")
		c.Requests++
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		resp.Body.Close()
		if err != nil {
			return err
		}
		if len(body) > maxBody {
			return errors.New("llm-stats response too large")
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			if wait <= 0 || wait > 120 {
				wait = 60
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(wait) * time.Second):
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			var env struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal(body, &env)
			msg := env.Error.Message
			if len(msg) > 200 {
				msg = msg[:200]
			}
			return &APIError{Status: resp.StatusCode, Code: env.Error.Code, Msg: msg}
		}
		dec := json.NewDecoder(strings.NewReader(string(body)))
		dec.UseNumber()
		return dec.Decode(out)
	}
	return &APIError{Status: http.StatusTooManyRequests, Code: "rate_limit_exceeded"}
}

// Wire types: only the fields Intel uses. Numbers stay json.Number so prices
// and scores are converted exactly.

type Ref struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type License struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	AllowCommercial bool   `json:"allow_commercial"`
	OpenSource      bool   `json:"open_source"`
}

type ProviderPricing struct {
	ProviderID    string       `json:"provider_id"`
	ProviderName  string       `json:"provider_name"`
	InputPerM     *json.Number `json:"input_price_per_m"`
	OutputPerM    *json.Number `json:"output_price_per_m"`
	ThroughputTPS *json.Number `json:"throughput_tps"`
	LatencyS      *json.Number `json:"latency_s"`
	Status        string       `json:"status"`
}

type Model struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Organization  Ref               `json:"organization"`
	Family        *Ref              `json:"family"`
	License       License           `json:"license"`
	OpenWeight    bool              `json:"open_weight"`
	ModelType     string            `json:"model_type"`
	Modalities    []string          `json:"modalities"`
	ContextWindow *int64            `json:"context_window"`
	ReleaseDate   *string           `json:"release_date"`
	Providers     []ProviderPricing `json:"providers"`
	URL           string            `json:"url"`
}

type Benchmark struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Categories []string    `json:"categories"`
	Modality   string      `json:"modality"`
	MaxScore   json.Number `json:"max_score"`
	URL        string      `json:"url"`
}

type Score struct {
	ModelID         string       `json:"model_id"`
	BenchmarkID     string       `json:"benchmark_id"`
	Score           json.Number  `json:"score"`
	MaxScore        json.Number  `json:"max_score"`
	NormalizedScore *json.Number `json:"normalized_score"`
	IsSelfReported  bool         `json:"is_self_reported"`
	Verified        bool         `json:"verified"`
	ScoredAt        string       `json:"scored_at"`
	URL             string       `json:"url"`
}

type Dataset struct {
	Models     []Model
	Benchmarks []Benchmark
	Scores     []Score
}

// Fetch reads the full catalog: all model pages, the benchmark list and all
// score pages.
func (c *Client) Fetch(ctx context.Context) (Dataset, error) {
	var ds Dataset
	var cursor string
	for page := 0; ; page++ {
		if page >= maxPages {
			return ds, errors.New("llm-stats models pagination did not terminate")
		}
		q := url.Values{"limit": {"200"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var resp struct {
			Models     []Model `json:"models"`
			NextCursor *string `json:"next_cursor"`
		}
		if err := c.get(ctx, "/v1/models", q, &resp); err != nil {
			return ds, fmt.Errorf("list models: %w", err)
		}
		ds.Models = append(ds.Models, resp.Models...)
		if resp.NextCursor == nil || *resp.NextCursor == "" || *resp.NextCursor == cursor {
			break
		}
		cursor = *resp.NextCursor
	}
	var benches struct {
		Benchmarks []Benchmark `json:"benchmarks"`
	}
	if err := c.get(ctx, "/v1/benchmarks", nil, &benches); err != nil {
		return ds, fmt.Errorf("list benchmarks: %w", err)
	}
	ds.Benchmarks = benches.Benchmarks
	cursor = ""
	for page := 0; ; page++ {
		if page >= maxPages {
			return ds, errors.New("llm-stats scores pagination did not terminate")
		}
		q := url.Values{"limit": {"500"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var resp struct {
			Scores     []Score `json:"scores"`
			NextCursor *string `json:"next_cursor"`
		}
		if err := c.get(ctx, "/v1/scores", q, &resp); err != nil {
			return ds, fmt.Errorf("list scores: %w", err)
		}
		ds.Scores = append(ds.Scores, resp.Scores...)
		if resp.NextCursor == nil || *resp.NextCursor == "" || *resp.NextCursor == cursor {
			break
		}
		cursor = *resp.NextCursor
	}
	return ds, nil
}

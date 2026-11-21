package newapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// RecordedUsage implements the publicly documented read-only Meta API v1.41.2 contract.
// Only the Worker should supply the ID returned by its protected binding reader.
// Job retry/backoff lives outside the transport. No settlement operation is invoked.
func (c *BenefitClient) RecordedUsage(ctx context.Context, bound uint64, expectedDay string) (*int64, error) {
	const path = "/api/internal/pulse/usage/yesterday"
	const role = "pulse-usage"
	if bound == 0 || c.role != "pulse-settlement" || len(strings.TrimSpace(string(c.secret))) < 32 {
		return nil, errors.New("usage unavailable")
	}
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if e != nil {
		return nil, e
	}
	var salt [16]byte
	if _, e = rand.Read(salt[:]); e != nil {
		return nil, e
	}
	who := strconv.FormatUint(bound, 10)
	nonce := hex.EncodeToString(salt[:])
	now := c.now().Unix()
	key := signHex([]byte(strings.TrimSpace(string(c.secret))), "meta-api:pulse-usage:yesterday:v1")
	mac := signHex([]byte(key), security.CanonicalPayload(request.Method, path, who, role, now, nonce, nil))
	for k, v := range map[string]string{security.HeaderUserID: who, security.HeaderRole: role, security.HeaderNonce: nonce, security.HeaderTimestamp: strconv.FormatInt(now, 10), security.HeaderSignature: mac} {
		request.Header.Set(k, v)
	}
	res, e := c.http.Do(request)
	if e != nil {
		return nil, errors.New("usage request unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("usage returned status %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 4097))
	if e != nil || len(b) > 4096 {
		return nil, errors.New("invalid usage response")
	}
	var row struct {
		Date, Completeness string
		Recorded           json.RawMessage `json:"recorded_consume_quota"`
	}
	if json.Unmarshal(b, &row) != nil || row.Date != expectedDay || row.Completeness != "unknown" || len(row.Recorded) == 0 {
		return nil, errors.New("invalid usage snapshot")
	}
	var recorded *int64
	if json.Unmarshal(row.Recorded, &recorded) != nil || recorded != nil && *recorded < 0 {
		return nil, errors.New("invalid usage amount")
	}
	return recorded, nil
}

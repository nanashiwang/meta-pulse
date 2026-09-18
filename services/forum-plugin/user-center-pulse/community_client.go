package pulse_user_center

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const communityServiceRole = "community-bff"
const maxCommunityResponseBytes = 1 << 20

// communityRequest constructs a new request with an independent least-privilege
// key. Browser Authorization, cookies, signatures and arbitrary headers never
// enter the request sent to Pulse.
func (c *PulseClient) communityRequest(ctx context.Context, method, path, externalID, idempotencyKey string, body []byte) ([]byte, int, error) {
	if c == nil || c.config == nil || !usableConfigSecret(c.config.CommunityBFFHMACSecret) || !canonicalCommunityID(externalID) {
		return nil, 0, errors.New("community client unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.config.PulseBaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	nonce, err := c.nonce()
	if err != nil {
		return nil, 0, err
	}
	timestamp := c.now().Unix()
	canonical := pulseCanonicalPayload(method, req.URL.EscapedPath(), externalID, communityServiceRole, timestamp, nonce, body)
	mac := hmac.New(sha256.New, []byte(c.config.CommunityBFFHMACSecret))
	_, _ = mac.Write([]byte(canonical))
	req.Header.Set(pulseHeaderUserID, externalID)
	req.Header.Set(pulseHeaderRole, communityServiceRole)
	req.Header.Set(pulseHeaderTimestamp, strconv.FormatInt(timestamp, 10))
	req.Header.Set(pulseHeaderNonce, nonce)
	req.Header.Set(pulseHeaderSignature, hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCommunityResponseBytes+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(data) > maxCommunityResponseBytes {
		return nil, resp.StatusCode, errors.New("community response too large")
	}
	return data, resp.StatusCode, nil
}

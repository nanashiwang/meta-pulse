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

// adminSettingsRequest can sign only the fixed settings endpoints. Browser
// credentials never reach Pulse and submitted settlement keys are not retained
// by the plugin or reused as a request-signing capability.
func (c *PulseClient) adminSettingsRequest(ctx context.Context, method, operation, adminID, key string, body []byte) ([]byte, int, error) {
	if c == nil || c.config == nil || !usableConfigSecret(c.config.AdminHMACSecret) || !canonicalCommunityID(adminID) {
		return nil, 0, errors.New("admin settings unavailable")
	}
	path := "/v1/internal/admin/settings"
	if operation == "periods" && (method == http.MethodGet || method == http.MethodPut) {
		path = "/v1/internal/admin/periods"
	} else if operation == "secret" && method == http.MethodPost {
		path += "/secret"
	} else if operation != "settings" || (method != http.MethodGet && method != http.MethodPut) {
		return nil, 0, errors.New("unsupported admin settings operation")
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
	mac := hmac.New(sha256.New, []byte(c.config.AdminHMACSecret))
	_, _ = mac.Write([]byte(pulseCanonicalPayload(method, req.URL.EscapedPath(), adminID, "admin", timestamp, nonce, body)))
	req.Header.Set(pulseHeaderUserID, adminID)
	req.Header.Set(pulseHeaderRole, "admin")
	req.Header.Set(pulseHeaderTimestamp, strconv.FormatInt(timestamp, 10))
	req.Header.Set(pulseHeaderNonce, nonce)
	req.Header.Set(pulseHeaderSignature, hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Accept", "application/json")
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPut {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAdminSettingsBytes+1))
	if err != nil || len(data) > maxAdminSettingsBytes {
		return nil, resp.StatusCode, errors.New("invalid admin settings response")
	}
	return data, resp.StatusCode, nil
}

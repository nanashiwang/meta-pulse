// Package security verifies signed admin requests from the forum plugin BFF.
// The canonical payload matches the Pulse internal contract:
//
//	METHOD \n path \n userID \n role \n unixSeconds \n nonce \n hex(sha256(body))
//
// Only the role "admin" is accepted, and the nonce is claimed only after the
// signature and time window check out.
package security

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderUserID    = "X-Intel-User-Id"
	HeaderRole      = "X-Intel-Role"
	HeaderTimestamp = "X-Intel-Timestamp"
	HeaderNonce     = "X-Intel-Nonce"
	HeaderSignature = "X-Intel-Signature"

	AllowedSkew = 2 * time.Minute
)

var (
	ErrInvalid = errors.New("invalid admin request signature")
	ErrReplay  = errors.New("replayed admin request")

	userIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,31}$`)
	noncePattern  = regexp.MustCompile(`^[0-9a-f]{32,128}$`)
)

type NonceStore interface {
	ClaimNonce(ctx context.Context, nonce string, expiresAt time.Time) (bool, error)
}

func CanonicalPayload(method, path, userID, role string, timestamp int64, nonce string, body []byte) string {
	digest := sha256.Sum256(body)
	return strings.Join([]string{
		strings.ToUpper(method), path, userID, role,
		strconv.FormatInt(timestamp, 10), nonce, hex.EncodeToString(digest[:]),
	}, "\n")
}

func Sign(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify authenticates the request, restores its body for the handler, and
// returns the admin's Answer user ID. Secrets are tried in order so a
// previous secret keeps working during rotation.
func Verify(req *http.Request, secrets [][]byte, now time.Time, maxBody int64, nonces NonceStore) (string, []byte, error) {
	usable := false
	for _, s := range secrets {
		if len(s) >= 32 {
			usable = true
		}
	}
	if !usable || req == nil || req.URL == nil {
		return "", nil, ErrInvalid
	}
	single := func(name string) (string, bool) {
		v := req.Header.Values(name)
		return strings.TrimSpace(strings.Join(v, "")), len(v) == 1
	}
	userID, ok1 := single(HeaderUserID)
	role, ok2 := single(HeaderRole)
	tsRaw, ok3 := single(HeaderTimestamp)
	nonce, ok4 := single(HeaderNonce)
	sig, ok5 := single(HeaderSignature)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || role != "admin" || !userIDPattern.MatchString(userID) || !noncePattern.MatchString(nonce) {
		return "", nil, ErrInvalid
	}
	ts, err := strconv.ParseInt(tsRaw, 10, 64)
	if err != nil {
		return "", nil, ErrInvalid
	}
	sent := time.Unix(ts, 0)
	if sent.Before(now.Add(-AllowedSkew)) || sent.After(now.Add(AllowedSkew)) {
		return "", nil, ErrInvalid
	}
	given, err := hex.DecodeString(sig)
	if err != nil || len(given) != sha256.Size {
		return "", nil, ErrInvalid
	}
	var body []byte
	if req.Body != nil {
		body, err = io.ReadAll(io.LimitReader(req.Body, maxBody+1))
		if err != nil || int64(len(body)) > maxBody {
			return "", nil, ErrInvalid
		}
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	payload := CanonicalPayload(req.Method, req.URL.EscapedPath(), userID, role, ts, nonce, body)
	matched := false
	for _, s := range secrets {
		if len(s) < 32 {
			continue
		}
		want, _ := hex.DecodeString(Sign(s, payload))
		if hmac.Equal(given, want) {
			matched = true
		}
	}
	if !matched {
		return "", nil, ErrInvalid
	}
	claimed, err := nonces.ClaimNonce(req.Context(), nonce, now.Add(2*AllowedSkew))
	if err != nil {
		return "", nil, err
	}
	if !claimed {
		return "", nil, ErrReplay
	}
	return userID, body, nil
}

package imageimport

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(c context.Context, n, h string) ([]netip.Addr, error) {
	return f(c, n, h)
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAddressBoundaries(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.100.100.200", "192.168.1.2", "198.18.0.1", "224.0.0.1", "0.0.0.0", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "64:ff9b::7f00:1", "2002:7f00:1::", "2001:db8::1", "3fff::1"} {
		if publicIP(netip.MustParseAddr(raw)) {
			t.Errorf("allowed %s", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(raw)) {
			t.Errorf("blocked public %s", raw)
		}
	}
}
func TestDialPinsDNSAndRejectsMixedAnswers(t *testing.T) {
	calls := 0
	addresses := []netip.Addr{netip.MustParseAddr("1.1.1.1")}
	resolve := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { calls++; return addresses, nil })
	dialed := ""
	dial := safeDial(resolve, func(_ context.Context, _, address string) (net.Conn, error) { dialed = address; return nil, nil })
	if _, err := dial(context.Background(), "tcp", "example.org:443"); err != nil || dialed != "1.1.1.1:443" || calls != 1 {
		t.Fatal(err, dialed, calls)
	}
	addresses = append(addresses, netip.MustParseAddr("127.0.0.1"))
	dialed = ""
	if _, err := dial(context.Background(), "tcp", "example.org:443"); err == nil || dialed != "" {
		t.Fatal("mixed DNS dialed")
	}
}
func TestFetcherValidationAndBounds(t *testing.T) {
	var pngData bytes.Buffer
	_ = png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	data := pngData.Bytes()
	code := 200
	calls := 0
	f := New()
	f.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
			t.Fatal("credentials forwarded")
		}
		return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: -1, Request: r}, nil
	})
	for _, raw := range []string{"http://example.org/a", "https://user:pass@example.org/a", "https://example.org:8443/a", "file:///tmp/a", "data:image/png;base64,x"} {
		if _, err := f.Fetch(context.Background(), raw); err == nil {
			t.Fatal(raw)
		}
	}
	if calls != 0 {
		t.Fatal("invalid URL fetched")
	}
	result, err := f.Fetch(context.Background(), "https://example.org/a#fragment")
	if err != nil || result.MIME != "image/png" || !bytes.Equal(result.Bytes, data) {
		t.Fatal(result, err)
	}
	for _, bad := range [][]byte{[]byte("<svg xmlns='x'></svg>"), []byte("<html>blocked</html>"), bytes.Repeat([]byte("x"), MaxBytes+1)} {
		data = bad
		if _, err = f.Fetch(context.Background(), "https://example.org/a"); err == nil {
			t.Fatal("bad image accepted")
		}
	}
	data = pngData.Bytes()
	code = 403
	if _, err = f.Fetch(context.Background(), "https://example.org/a"); err == nil {
		t.Fatal("non-200 accepted")
	}
}
func TestRedirectRevalidatedAndHeadersNotLeaked(t *testing.T) {
	f := New()
	calls := 0
	f.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://127.0.0.1/secret"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, err := f.Fetch(context.Background(), "https://example.org/a"); err == nil || calls != 1 {
		t.Fatal("redirect accepted", calls, err)
	}
	// Real HTTPS transport refuses loopback without touching the target listener.
	hit := false
	s := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer s.Close()
	if _, err := New().Fetch(context.Background(), s.URL); err == nil || hit {
		t.Fatal("private target accepted")
	}
}

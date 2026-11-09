package pulse_user_center

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	brotlimiddleware "github.com/anargu/gin-brotli"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
)

func TestIntelAdminTransport(t *testing.T) {
	data := []byte(`{"payload":"` + strings.Repeat("x", 4096) + `"}`)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	defer api.Close()
	previous := intelAdminConfig
	intelAdminConfig = func() intelAdminTarget {
		return intelAdminTarget{BaseURL: api.URL, Secret: strings.Repeat("i", 32)}
	}
	defer func() { intelAdminConfig = previous }()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(brotlimiddleware.Brotli(brotlimiddleware.DefaultCompression))
	uc := &UserCenter{Config: validPluginConfig(), Guard: &adminTestGuard{}}
	uc.registerIntelAdmin(r.Group("/answer/admin/api"), func(*gin.Context) (string, error) { return "7", nil }, func() string { return "https://metar.example.test" })
	answer := httptest.NewServer(r)
	defer answer.Close()
	target, err := url.Parse(answer.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(target))
	defer proxy.Close()

	for _, encoding := range []string{"identity", "br"} {
		req, _ := http.NewRequest(http.MethodGet, proxy.URL+"/answer/admin/api/metar/intel/sources", nil)
		req.Header.Set("Authorization", "Bearer session")
		req.Header.Set("X-Metar-Request", "1")
		req.Header.Set("Accept-Encoding", encoding)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("encoding=%s: incomplete HTTP response: %v", encoding, readErr)
		}
		if encoding == "br" {
			if resp.Header.Get("Content-Encoding") != "br" {
				t.Fatal("test did not exercise Brotli middleware")
			}
			body, err = io.ReadAll(brotli.NewReader(bytes.NewReader(body)))
			if err != nil {
				t.Fatalf("encoding=%s: invalid Brotli body: %v", encoding, err)
			}
		}
		if resp.StatusCode != http.StatusOK || !bytes.Equal(body, data) {
			t.Fatalf("encoding=%s: status=%d body length=%d, want %d", encoding, resp.StatusCode, len(body), len(data))
		}
	}
}

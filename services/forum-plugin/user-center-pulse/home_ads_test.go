package pulse_user_center

import (
	"bytes"
	brotlimiddleware "github.com/anargu/gin-brotli"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHomeAdsPrivateOperationsRequireSession(t *testing.T) {
	uc := &UserCenter{}
	for _, op := range []string{"manage", "preview", "update", "create", "delete"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		uc.homeAds(op, func() string { return "https://example.org" })(c)
		if w.Code != 403 {
			t.Errorf("unauthenticated %s: got %d", op, w.Code)
		}
	}
}

func TestHomeAdImageTransport(t *testing.T) {
	payload := append([]byte{0xff, 0xd8}, bytes.Repeat([]byte("ad-image-test"), 8192)...)
	r := gin.New()
	r.Use(brotlimiddleware.Brotli(brotlimiddleware.DefaultCompression))
	r.GET("/image", func(c *gin.Context) { writeHomeAdJPEG(c, payload) })
	s := httptest.NewServer(r)
	defer s.Close()
	for _, enc := range []string{"identity", "br"} {
		req, _ := http.NewRequest("GET", s.URL+"/image", nil)
		req.Header.Set("Accept-Encoding", enc)
		resp, err := s.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var reader io.Reader = resp.Body
		if enc == "br" {
			if resp.Header.Get("Content-Encoding") != "br" {
				t.Fatal("Brotli not exercised")
			}
			reader = brotli.NewReader(reader)
		}
		body, err := io.ReadAll(reader)
		resp.Body.Close()
		if err != nil || !bytes.Equal(body, payload) || resp.Header.Get("Content-Type") != "image/jpeg" {
			t.Fatalf("%s: incomplete image (%d bytes), %v", enc, len(body), err)
		}
	}
}

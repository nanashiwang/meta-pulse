package pulse_user_center

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	brotlimiddleware "github.com/anargu/gin-brotli"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/imageimport"
)

type transportImageDownloader struct{ data []byte }

func (d transportImageDownloader) Fetch(context.Context, string) (imageimport.Image, error) {
	return imageimport.Image{Bytes: d.data, MIME: "image/png"}, nil
}

// The real Answer server installs Brotli before plugin routes. A recorder alone
// does not enforce HTTP Content-Length and missed truncated responses behind nginx.
func TestImageImportTransport(t *testing.T) {
	for _, size := range []int{1, 256} {
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		_, _ = rand.New(rand.NewSource(42)).Read(img.Pix)
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, img); err != nil {
			t.Fatal(err)
		}
		data := encoded.Bytes()
		r := gin.New()
		r.Use(brotlimiddleware.Brotli(brotlimiddleware.DefaultCompression))
		r.POST("/fetch", imageImportHandler(func(*gin.Context) (string, error) { return "1", nil },
			func(context.Context, string) error { return nil }, func() string { return "https://community.test" },
			transportImageDownloader{data}, &imageLimiter{}))
		upstream := httptest.NewServer(r)
		defer upstream.Close()
		target, _ := url.Parse(upstream.URL)
		proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(target))
		defer proxy.Close()
		for _, host := range []string{upstream.URL, proxy.URL} {
			for _, encoding := range []string{"identity", "br"} {
				req, _ := http.NewRequest("POST", host+"/fetch", strings.NewReader(`{"url":"https://example.org/image.png"}`))
				req.Header.Set("Authorization", "session")
				req.Header.Set("Origin", "https://community.test")
				req.Header.Set("X-Metar-Request", "1")
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept-Encoding", encoding)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr != nil {
					t.Fatalf("size=%d encoding=%s: incomplete HTTP response: %v", size, encoding, readErr)
				}
				if encoding == "br" {
					if resp.Header.Get("Content-Encoding") != "br" {
						t.Fatal("test did not exercise Answer Brotli middleware")
					}
					body, err = io.ReadAll(brotli.NewReader(bytes.NewReader(body)))
					if err != nil {
						t.Fatal(err)
					}
				}
				if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(body, data) {
					t.Fatalf("size=%d encoding=%s: image bytes or headers changed", size, encoding)
				}
			}
		}
	}
}

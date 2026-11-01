// Package imageimport downloads bounded public raster images. It owns no storage
// or user identity: the authenticated adapter and Answer uploader own those.
package imageimport

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

const MaxBytes = 5 << 20

var ErrImage = errors.New("image_unavailable")

type Image struct {
	Bytes []byte
	MIME  string
}
type Fetcher struct{ client *http.Client }

// Special-use ranges must never become egress destinations, even via DNS or
// redirects. IPv6 is restricted to currently allocated global unicast space.
var blocked = func() []netip.Prefix {
	var result []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/3", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20"} {
		result = append(result, netip.MustParsePrefix(s))
	}
	return result
}()

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.Zone() != "" {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, p := range blocked {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

func validURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && (u.Port() == "" || u.Port() == "443") && !strings.ContainsAny(u.Host, "%\\") && len(u.String()) <= 4096
}

type resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

func safeDial(resolve resolver, dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" {
			return nil, ErrImage
		}
		ips, err := resolve.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, ErrImage
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, ErrImage
			}
		}
		// Dial the validated numeric IP, never resolve the hostname a second time.
		for _, ip := range ips {
			conn, e := dial(ctx, "tcp", net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
			err = e
			if ctx.Err() != nil {
				break
			}
		}
		return nil, err
	}
}

func New() *Fetcher {
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	transport := &http.Transport{Proxy: nil, DialContext: safeDial(net.DefaultResolver, dialer.DialContext), TLSHandshakeTimeout: 4 * time.Second, ResponseHeaderTimeout: 8 * time.Second, DisableKeepAlives: true, DisableCompression: true, MaxResponseHeaderBytes: 16 << 10}
	return &Fetcher{client: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !validURL(req.URL) {
			return ErrImage
		}
		req.Header.Del("Referer")
		return nil
	}}}
}

func (f *Fetcher) Fetch(ctx context.Context, raw string) (Image, error) {
	u, err := url.Parse(raw)
	if err != nil || !validURL(u) {
		return Image{}, ErrImage
	}
	u.Fragment = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Image{}, ErrImage
	}
	req.Header.Set("Accept", "image/png,image/jpeg,image/gif,image/webp")
	req.Header.Set("User-Agent", "MetaR-Image-Import/1.0")
	resp, err := f.client.Do(req)
	if err != nil {
		return Image{}, ErrImage
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > MaxBytes {
		return Image{}, ErrImage
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil || len(data) > MaxBytes {
		return Image{}, ErrImage
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 20_000_000 {
		return Image{}, ErrImage
	}
	mime := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}[format]
	if mime == "" {
		return Image{}, ErrImage
	}
	return Image{Bytes: data, MIME: mime}, nil
}

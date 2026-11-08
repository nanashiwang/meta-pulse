package probe

import (
	"encoding/json"
	"github.com/miekg/dns"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func request(s *Service, path, body, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Metar-DNS-Probe", "1")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestSessionBoundary(t *testing.T) {
	s := New()
	now := time.Now()
	s.now = func() time.Time { return now }
	w := request(s, "/metar/api/tools/dns/start", "{}", Origin)
	if w.Code != 200 {
		t.Fatal(w)
	}
	var start struct{ Token, Hostname string }
	if err := json.Unmarshal(w.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	if !validToken(start.Token) || !strings.HasSuffix(start.Hostname, "."+strings.TrimSuffix(Zone, ".")) {
		t.Fatal(start)
	}
	if strings.Contains(start.Hostname, start.Token) {
		t.Fatal("lookup secret leaked into DNS")
	}
	if request(s, "/metar/api/tools/dns/start", "{}", "https://evil.example").Code != 403 {
		t.Fatal("cross origin accepted")
	}
	for _, body := range []string{"{} {}", `{"typo":1}`, strings.Repeat("x", 257)} {
		if request(s, "/metar/api/tools/dns/start", body, Origin).Code != 400 {
			t.Fatal(body)
		}
	}
	if request(s, "/metar/api/tools/dns/result", `{"token":"`+strings.Split(start.Hostname, ".")[0]+`"}`, Origin).Code != 404 {
		t.Fatal("DNS name grants read access")
	}
	now = now.Add(TTL + time.Second)
	s.Sweep()
	if len(s.sessions) != 0 || len(s.names) != 0 {
		t.Fatal("expired retained")
	}
	if request(s, "/metar/api/tools/dns/result", `{"token":"`+start.Token+`"}`, Origin).Code != 404 {
		t.Fatal("expired accepted")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cacheable")
	}
}
func TestAuthorityNetworkAndObservation(t *testing.T) {
	for _, network := range []string{"udp", "tcp"} {
		t.Run(network, func(t *testing.T) {
			s := New()
			key, name, err := s.issue()
			if err != nil {
				t.Fatal(err)
			}
			server := &dns.Server{Handler: s}
			var address string
			if network == "udp" {
				conn, e := net.ListenPacket("udp", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				server.PacketConn = conn
				address = conn.LocalAddr().String()
			} else {
				conn, e := net.Listen("tcp", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				server.Listener = conn
				address = conn.Addr().String()
			}
			ready := make(chan struct{})
			server.NotifyStartedFunc = func() { close(ready) }
			go server.ActivateAndServe()
			<-ready
			t.Cleanup(func() { _ = server.Shutdown() })
			client := &dns.Client{Net: network, Timeout: time.Second}
			for _, tc := range []struct {
				name  string
				typ   uint16
				rcode int
				aa    bool
			}{{Zone, dns.TypeNS, 0, true}, {Zone, dns.TypeSOA, 0, true}, {Zone, dns.TypeAAAA, 0, true}, {"elsewhere.example.", dns.TypeA, dns.RcodeRefused, false}, {name, dns.TypeA, dns.RcodeNameError, true}, {name, dns.TypeAAAA, dns.RcodeNameError, true}} {
				q := new(dns.Msg)
				q.SetQuestion(tc.name, tc.typ)
				r, _, err := client.Exchange(q, address)
				if err != nil {
					t.Fatal(err)
				}
				if r.Rcode != tc.rcode || r.Authoritative != tc.aa || r.RecursionAvailable {
					t.Fatalf("bad reply: %v", r)
				}
			}
			w := request(s, "/metar/api/tools/dns/result", `{"token":"`+key+`"}`, Origin)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "127.0.0.1") {
				t.Fatal(w.Body.String())
			}
			if len(s.sessions[key].resolvers) != 1 {
				t.Fatal("not deduplicated")
			}
		})
	}
}
func TestIssuanceBoundAndConcurrency(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _, _ = s.issue() }()
	}
	wg.Wait()
	if len(s.sessions) != 120 {
		t.Fatalf("limit: %d", len(s.sessions))
	}
	if _, _, err := s.issue(); err == nil {
		t.Fatal("limit bypass")
	}
}

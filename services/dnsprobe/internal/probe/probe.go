// Package probe implements a short-lived authoritative resolver observation.
// It never resolves arbitrary names or stores visitor identity or query logs.
package probe

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const Zone = "dnsprobe.metarhub.com."
const Origin = "https://metarhub.com"
const TTL = 90 * time.Second
const capacity = 2048

type session struct {
	name      string
	expires   time.Time
	resolvers map[string]bool
}
type Service struct {
	mu       sync.Mutex
	sessions map[string]*session
	names    map[string]string
	now      func() time.Time
	// Global bounded limiter: HTTP issuance and DNS responses, not unbounded per-IP maps.
	issueWindow, dnsWindow time.Time
	issues, responses      int
}

func New() *Service {
	return &Service{sessions: make(map[string]*session), names: make(map[string]string), now: time.Now}
}
func token() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
func (s *Service) cleanup(now time.Time) {
	for key, v := range s.sessions {
		if !now.Before(v.expires) {
			delete(s.names, v.name)
			delete(s.sessions, key)
		}
	}
}

// Sweep bounds retention even when no further HTTP or DNS request arrives.
func (s *Service) Sweep() { s.mu.Lock(); defer s.mu.Unlock(); s.cleanup(s.now()) }
func (s *Service) issue() (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.cleanup(now)
	if now.Sub(s.issueWindow) >= time.Minute {
		s.issueWindow = now
		s.issues = 0
	}
	if len(s.sessions) >= capacity || s.issues >= 120 {
		return "", "", errors.New("busy")
	}
	key, err := token()
	if err != nil {
		return "", "", err
	}
	label, err := token()
	if err != nil {
		return "", "", err
	}
	name := label + "." + Zone
	s.sessions[key] = &session{name: name, expires: now.Add(TTL), resolvers: make(map[string]bool)}
	s.names[name] = key
	s.issues++
	return key, name, nil
}
func validToken(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json")
	respond := func(status int, value any) { w.WriteHeader(status); _ = json.NewEncoder(w).Encode(value) }
	if r.URL.Path == "/readyz" && (r.Method == "GET" || r.Method == "HEAD") {
		respond(200, map[string]bool{"ready": true})
		return
	}
	// Both endpoints POST so the private lookup key never appears in access URLs.
	if r.Method != "POST" {
		respond(405, map[string]string{"error": "method_not_allowed"})
		return
	}
	if r.Header.Get("Origin") != Origin || r.Header.Get("X-Metar-DNS-Probe") != "1" {
		respond(403, map[string]string{"error": "forbidden"})
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		respond(415, map[string]string{"error": "invalid_content_type"})
		return
	}
	if r.URL.RawQuery != "" {
		respond(400, map[string]string{"error": "invalid_query"})
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		respond(400, map[string]string{"error": "invalid_body"})
		return
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		respond(400, map[string]string{"error": "invalid_body"})
		return
	}
	switch r.URL.Path {
	case "/metar/api/tools/dns/start":
		if body.Token != "" {
			respond(400, map[string]string{"error": "invalid_body"})
			return
		}
		key, name, err := s.issue()
		if err != nil {
			respond(429, map[string]string{"error": "busy"})
			return
		}
		respond(200, map[string]any{"token": key, "hostname": strings.TrimSuffix(name, "."), "expires_in": int(TTL.Seconds())})
	case "/metar/api/tools/dns/result":
		if !validToken(body.Token) {
			respond(400, map[string]string{"error": "invalid_token"})
			return
		}
		s.mu.Lock()
		s.cleanup(s.now())
		v, ok := s.sessions[body.Token]
		addresses := []string{}
		if ok {
			for ip := range v.resolvers {
				addresses = append(addresses, ip)
			}
		}
		s.mu.Unlock()
		if !ok {
			respond(404, map[string]string{"error": "expired"})
			return
		}
		sort.Strings(addresses)
		respond(200, map[string]any{"resolvers": addresses})
	default:
		respond(404, map[string]string{"error": "not_found"})
	}
}

func soa() dns.RR {
	return &dns.SOA{Hdr: dns.RR_Header{Name: Zone, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 0}, Ns: "ns1.metarhub.com.", Mbox: "hostmaster.metarhub.com.", Serial: 1, Refresh: 3600, Retry: 600, Expire: 86400, Minttl: 0}
}
func (s *Service) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	// Rate bound applies to all queries including random nonexistent names. No recursion/ANY amplification.
	s.mu.Lock()
	now := s.now()
	s.cleanup(now)
	if now.Sub(s.dnsWindow) >= time.Second {
		s.dnsWindow = now
		s.responses = 0
	}
	s.responses++
	allowed := s.responses <= 300
	s.mu.Unlock()
	if !allowed || r.Response {
		return
	}
	m := new(dns.Msg)
	m.SetReply(r)
	m.RecursionAvailable = false
	m.Compress = true
	if r.Opcode != dns.OpcodeQuery || len(r.Question) != 1 {
		m.Rcode = dns.RcodeFormatError
		_ = w.WriteMsg(m)
		return
	}
	q := r.Question[0]
	name := strings.ToLower(q.Name)
	if q.Qclass != dns.ClassINET || !dns.IsSubDomain(Zone, name) {
		m.Rcode = dns.RcodeRefused
		_ = w.WriteMsg(m)
		return
	}
	m.Authoritative = true
	if name == Zone {
		switch q.Qtype {
		case dns.TypeSOA:
			m.Answer = []dns.RR{soa()}
		case dns.TypeNS:
			m.Answer = []dns.RR{&dns.NS{Hdr: dns.RR_Header{Name: Zone, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 300}, Ns: "ns1.metarhub.com."}}
		default:
			m.Ns = []dns.RR{soa()}
		}
	} else {
		// All probe names are NXDOMAIN. DNS is sufficient; no TLS certificate, web server or tracking pixel is needed.
		m.Rcode = dns.RcodeNameError
		m.Ns = []dns.RR{soa()}
		if q.Qtype == dns.TypeA || q.Qtype == dns.TypeAAAA || q.Qtype == dns.TypeHTTPS {
			host, _, err := net.SplitHostPort(w.RemoteAddr().String())
			ip, e := netip.ParseAddr(host)
			if err == nil && e == nil {
				s.mu.Lock()
				if key, ok := s.names[name]; ok {
					v := s.sessions[key]
					if now.Before(v.expires) && len(v.resolvers) < 32 {
						v.resolvers[ip.Unmap().String()] = true
					}
				}
				s.mu.Unlock()
			}
		}
	}
	// Refuse EDNS bulk options and transfer requests; responses stay small.
	_ = w.WriteMsg(m)
}

package core

import (
	"net/http"
	"testing"
)

// The API binds loopback only, so RealIP's trusted-peer test always passes in
// production and the choice of header is the entire protection. Caddy 2.11
// overwrites X-Forwarded-For but passes a client's X-Real-IP straight through,
// so reading X-Real-IP let a caller choose its own rate-limit bucket.
func TestRealIPIgnoresClientSuppliedHeaders(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		realIP     string
		forwarded  string
		want       string
	}{
		{"X-Real-IP is never trusted", "127.0.0.1:9000", "9.9.9.9", "", "127.0.0.1:9000"},
		{"X-Real-IP loses to X-Forwarded-For", "127.0.0.1:9000", "9.9.9.9", "203.0.113.7", "203.0.113.7"},
		{"proxy-written entry is the right-most", "127.0.0.1:9000", "", "1.2.3.4, 203.0.113.7", "203.0.113.7"},
		{"single entry is the client", "127.0.0.1:9000", "", "203.0.113.7", "203.0.113.7"},
		{"surrounding spaces are trimmed", "127.0.0.1:9000", "", "1.2.3.4 ,  203.0.113.7 ", "203.0.113.7"},
		{"no headers falls back to the peer", "127.0.0.1:9000", "", "", "127.0.0.1:9000"},
		{"untrusted peer keeps its own address", "203.0.113.9:4000", "9.9.9.9", "1.2.3.4", "203.0.113.9:4000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := http.NewRequest(http.MethodGet, "/api/auth/login", nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			r.RemoteAddr = tc.remoteAddr
			if tc.realIP != "" {
				r.Header.Set("X-Real-IP", tc.realIP)
			}
			if tc.forwarded != "" {
				r.Header.Set("X-Forwarded-For", tc.forwarded)
			}
			if got := RealIP(r); got != tc.want {
				t.Errorf("RealIP() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A rotating spoofed header must not buy a fresh bucket: every request from one
// peer keys to the same value, which is what makes the 5/min login limiter fire.
func TestRealIPBucketIsStableUnderSpoofing(t *testing.T) {
	first := ""
	for _, spoof := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
		r, _ := http.NewRequest(http.MethodGet, "/api/auth/login", nil)
		r.RemoteAddr = "127.0.0.1:9000"
		r.Header.Set("X-Real-IP", spoof)
		got := RealIP(r)
		if first == "" {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("spoofed X-Real-IP %q changed the bucket: %q then %q", spoof, first, got)
		}
	}
}

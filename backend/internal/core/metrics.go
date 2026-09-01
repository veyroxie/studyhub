package core

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Lightweight in-process metrics, exposed at /metrics in Prometheus text
// format. We avoid pulling in the client_golang dep — the surface we need
// is small and a stdlib-only collector keeps the binary lean.
//
// Tracked per (method, path-template, status_class):
//   http_requests_total                — counter
//   http_request_duration_seconds_*    — p50, p90, p99 (sampled reservoir)
//
// Plus a few process gauges (goroutines, uptime).

type metricKey struct {
	method, route, statusClass string
}

type histReservoir struct {
	mu      sync.Mutex
	samples []float64
	cap     int
	next    int
}

func newReservoir(n int) *histReservoir {
	return &histReservoir{samples: make([]float64, 0, n), cap: n}
}

// add stores a sample. We use a simple ring buffer (not classical reservoir
// sampling) — for /metrics scraping the last N requests is more useful than
// uniform-random over all-time.
func (h *histReservoir) add(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.samples) < h.cap {
		h.samples = append(h.samples, v)
		return
	}
	h.samples[h.next] = v
	h.next = (h.next + 1) % h.cap
}

func (h *histReservoir) quantile(q float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.samples) == 0 {
		return 0
	}
	cp := append([]float64(nil), h.samples...)
	sort.Float64s(cp)
	i := int(float64(len(cp)-1) * q)
	return cp[i]
}

var (
	metricsMu    sync.RWMutex
	reqCounts    = map[metricKey]uint64{}
	reqDurations = map[metricKey]*histReservoir{}
	BootTime     = time.Now()
)

// metricsMiddleware records a counter + duration sample for every request.
// `route` falls back to URL path; if you want stable cardinality, route via
// chi's RouteContext (which carries the template). chi exposes
// chi.RouteContext(r.Context()).RoutePattern() after the router resolves.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		dur := time.Since(start).Seconds()
		// /metrics itself doesn't count — would self-pollute the histogram
		// during scrapes.
		if r.URL.Path == "/metrics" {
			return
		}
		statusClass := strconv.Itoa(rw.status/100) + "xx"
		k := metricKey{method: r.Method, route: routeTemplate(r), statusClass: statusClass}
		metricsMu.Lock()
		reqCounts[k]++
		h, ok := reqDurations[k]
		if !ok {
			h = newReservoir(1024)
			reqDurations[k] = h
		}
		metricsMu.Unlock()
		h.add(dur)
	})
}

// statusRecorder wraps the ResponseWriter to capture the status code.
//
// Embedding the http.ResponseWriter INTERFACE satisfies ResponseWriter but not
// the optional interfaces a handler may assert for: the wrapper type has no
// Hijack method, so `w.(http.Hijacker)` fails even though the writer
// underneath supports it. That silently broke every WebSocket upgrade in
// production ("response does not implement http.Hijacker") — live updates and
// parent check-in notifications were dead, and nothing failed loudly because
// the upgrade error is just a 500 on /ws.
//
// Hijack and Flush are declared explicitly because callers assert for them
// directly (gorilla/websocket does). Unwrap covers everything else: it lets
// http.ResponseController reach the underlying writer for any optional
// behaviour added later, so a future streaming API needs no change here.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(s int) {
	r.status = s
	r.ResponseWriter.WriteHeader(s)
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("metrics: underlying writer %T is not an http.Hijacker", r.ResponseWriter)
	}
	return hj.Hijack()
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap is what http.ResponseController uses to find the real writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// routeTemplate returns chi's route pattern when available (e.g.
// "/api/students/{id}"), falling back to the raw URL path. This keeps
// cardinality bounded — without it we'd get one series per student id.
func routeTemplate(r *http.Request) string {
	// chi.RouteContext is the canonical way, but to avoid an import cycle
	// we duck-type via the request context. Pragmatic fallback to URL.Path
	// when the route hasn't been resolved (e.g. 404s).
	if rt := chiRoutePattern(r); rt != "" {
		return rt
	}
	return r.URL.Path
}

// handleMetrics renders the in-process metrics as Prometheus text. Mounted
// at /metrics. No auth — scrapers run inside the deploy network only;
// production must firewall this port from the public.
func HandleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	// snapshot under lock then release before writing
	metricsMu.RLock()
	counts := make(map[metricKey]uint64, len(reqCounts))
	hists := make(map[metricKey]*histReservoir, len(reqDurations))
	for k, v := range reqCounts {
		counts[k] = v
	}
	for k, v := range reqDurations {
		hists[k] = v
	}
	metricsMu.RUnlock()

	fmt.Fprintf(w, "# HELP http_requests_total Total HTTP requests by method, route, status class\n")
	fmt.Fprintf(w, "# TYPE http_requests_total counter\n")
	for k, v := range counts {
		fmt.Fprintf(w, "http_requests_total{method=%q,route=%q,status=%q} %d\n", k.method, k.route, k.statusClass, v)
	}

	fmt.Fprintf(w, "# HELP http_request_duration_seconds Request duration quantiles (last 1024 reqs)\n")
	fmt.Fprintf(w, "# TYPE http_request_duration_seconds summary\n")
	for k, h := range hists {
		fmt.Fprintf(w, "http_request_duration_seconds{method=%q,route=%q,status=%q,quantile=\"0.5\"} %.6f\n", k.method, k.route, k.statusClass, h.quantile(0.5))
		fmt.Fprintf(w, "http_request_duration_seconds{method=%q,route=%q,status=%q,quantile=\"0.9\"} %.6f\n", k.method, k.route, k.statusClass, h.quantile(0.9))
		fmt.Fprintf(w, "http_request_duration_seconds{method=%q,route=%q,status=%q,quantile=\"0.99\"} %.6f\n", k.method, k.route, k.statusClass, h.quantile(0.99))
	}

	fmt.Fprintf(w, "# HELP studyhub_uptime_seconds Seconds since process start\n")
	fmt.Fprintf(w, "# TYPE studyhub_uptime_seconds gauge\n")
	fmt.Fprintf(w, "studyhub_uptime_seconds %.0f\n", time.Since(BootTime).Seconds())
}

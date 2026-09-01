package core

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

var errHijackProbe = errors.New("probe writer: hijack not performed")

// hijackableRecorder is an httptest.ResponseRecorder that also claims to be a
// Hijacker and Flusher, standing in for the real net/http writer. If a
// middleware passes this through intact, the assertions below hold; if it
// wraps it in a type lacking those methods, they fail.
type hijackableRecorder struct {
	*httptest.ResponseRecorder
}

func (h hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errHijackProbe
}

// TestMiddlewareKeepsWriterHijackable guards a whole class of outage rather
// than one instance.
//
// A middleware that wraps http.ResponseWriter to observe the response (to
// record a status code, say) hides the optional interfaces of the writer
// underneath unless it re-declares them. When that happened to
// MetricsMiddleware, every WebSocket upgrade in production failed with
// "response does not implement http.Hijacker" — live updates and parent
// check-in notifications were dead for months, and nothing alerted because a
// failed upgrade is just a 500 on one endpoint.
//
// Any middleware added to the list below is checked automatically. If you add
// one that wraps the writer, give the wrapper Hijack, Flush and Unwrap (see
// statusRecorder) rather than deleting the case here.
func TestMiddlewareKeepsWriterHijackable(t *testing.T) {
	middlewares := map[string]func(http.Handler) http.Handler{
		"MetricsMiddleware": MetricsMiddleware,
		"SecurityHeaders":   SecurityHeaders,
		"RequestID":         RequestID,
		"MaxBodySize":       MaxBodySize,
	}

	for name, mw := range middlewares {
		t.Run(name, func(t *testing.T) {
			var sawHijacker, sawFlusher bool
			probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, sawHijacker = w.(http.Hijacker)
				_, sawFlusher = w.(http.Flusher)
			})

			rec := hijackableRecorder{httptest.NewRecorder()}
			mw(probe).ServeHTTP(rec, httptest.NewRequest("GET", "/ws", nil))

			if !sawHijacker {
				t.Errorf("%s hides http.Hijacker from the handler — this breaks every WebSocket upgrade", name)
			}
			if !sawFlusher {
				t.Errorf("%s hides http.Flusher from the handler — this breaks streaming responses", name)
			}
		})
	}
}

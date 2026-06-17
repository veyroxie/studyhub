package core

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// chiRoutePattern returns the resolved route template (e.g.
// "/api/students/{id}") when chi has matched the request, or "" before
// routing. Kept in its own file so metrics.go stays free of the chi
// import — easier to lift out later if we add a richer metrics package.
func chiRoutePattern(r *http.Request) string {
	rc := chi.RouteContext(r.Context())
	if rc == nil {
		return ""
	}
	return rc.RoutePattern()
}

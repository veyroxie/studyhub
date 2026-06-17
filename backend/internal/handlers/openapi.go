package handlers

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var openAPISpec []byte

// handleOpenAPI serves the embedded OpenAPI 3.0 spec. Mounted public so
// integrators / API client generators can fetch it without auth. The
// spec describes the public + cookie-auth'd endpoints; signature-secret
// webhook bodies aren't documented in detail.
func HandleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write(openAPISpec)
}

package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestARefusalReachesThePersonButADatabaseErrorDoesNot(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/classes", nil)
	w := httptest.NewRecorder()
	respondCheckError(w, r, fmt.Errorf("checking: %w", userError("unknown pricing category: X")), http.StatusBadRequest)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unknown pricing category") {
		t.Errorf("refusal: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	respondCheckError(w, r, errors.New(`pq: relation "pricing_categories" does not exist`), http.StatusBadRequest)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "relation") {
		t.Errorf("internal error leaked: %d %s", w.Code, w.Body.String())
	}
}

package core

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMFACodesCannotBeGuessedFast(t *testing.T) {
	h := RateLimitMFA(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	codes := []int{}
	for i := 0; i < 7; i++ {
		req := httptest.NewRequest("POST", "/api/auth/mfa/confirm", nil)
		req.RemoteAddr = "203.0.113.9:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		codes = append(codes, w.Code)
	}
	if codes[4] != http.StatusOK || codes[5] != http.StatusTooManyRequests {
		t.Fatalf("codes = %v, want five allowed then 429", codes)
	}
}

package handlers

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Admins (Nadine, Chiying) run the centre; only the DEVELOPER_EMAILS owner sees
// technical screens. The server says which, so no email is hardcoded client-side.
func TestOnlyTheConfiguredDeveloperIsFlagged(t *testing.T) {
	t.Setenv("DEVELOPER_EMAILS", "someone-else@example.com, ADMIN@studyhub.com ")
	r, cleanup := setupTestApp(t)
	defer cleanup()
	for _, tc := range []struct {
		email, password string
		want            bool
	}{
		{"admin@studyhub.com", "admin123", true},
		{"seeduser27@example.com", "parent123", false},
	} {
		w := doRequest(r, "POST", "/api/auth/login", "", map[string]string{"email": tc.email, "password": tc.password})
		var resp struct {
			Developer bool `json:"developer"`
		}
		json.NewDecoder(w.Body).Decode(&resp)
		if w.Code != http.StatusOK || resp.Developer != tc.want {
			t.Errorf("%s: code %d developer=%v, want %v", tc.email, w.Code, resp.Developer, tc.want)
		}
	}
}

func TestNobodyIsADeveloperWhenTheListIsEmpty(t *testing.T) {
	t.Setenv("DEVELOPER_EMAILS", "")
	r, cleanup := setupTestApp(t)
	defer cleanup()
	w := doRequest(r, "POST", "/api/auth/login", "", map[string]string{"email": "admin@studyhub.com", "password": "admin123"})
	var resp struct {
		Developer bool `json:"developer"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Developer {
		t.Fatal("an admin was flagged developer with no DEVELOPER_EMAILS set")
	}
}

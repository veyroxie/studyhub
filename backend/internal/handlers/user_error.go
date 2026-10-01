package handlers

import (
	"errors"
	"net/http"

	"studyhub/internal/core"
)

// userError is a refusal written for the person at the screen. Any other error is
// internal: its text can carry SQL or driver detail and never reaches the client.
type userError string

func (e userError) Error() string { return string(e) }

// respondCheckError answers a failed check: a userError as written with status, anything else as a logged 500.
func respondCheckError(w http.ResponseWriter, r *http.Request, err error, status int) {
	var ue userError
	if errors.As(err, &ue) {
		core.RespondError(w, ue.Error(), status)
		return
	}
	core.LogFromReq(r).Error("check failed", "err", err, "path", r.URL.Path)
	core.RespondError(w, "server error, please try again", http.StatusInternalServerError)
}

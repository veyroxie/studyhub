package handlers

import "studyhub/internal/core"

// goSafe runs fn in a detached goroutine with panic recovery.
//
// chi's Recoverer middleware only wraps the request goroutine, so a panic in a
// fire-and-forget task (push delivery, transactional email) would take down the
// whole API process for every user. Background work must never be able to do
// that — log it and move on, exactly like the cron path's safeRun.
func goSafe(task string, fn func()) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				core.Logger.Error("panic in background task", "task", task, "err", rec)
			}
		}()
		fn()
	}()
}

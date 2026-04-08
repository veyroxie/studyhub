package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
)

// logger is the package-level structured logger. Initialised by initLogger()
// in main(). Production uses JSON output (greppable, ships well to log
// aggregators); development uses human-readable text.
//
// Named `logger` (not `log`) so it doesn't shadow Go's stdlib `log` package
// — main.go still uses log.Fatalf for startup errors, which is fine.
//
// Use it like:
//
//	logger.Info("invoice paid", "invoice_id", id, "amount", amount)
//	logger.Error("could not save invoice", "err", err, "student_id", sid)
//
// For request-scoped logging that includes the request_id automatically,
// use logFromCtx(ctx) inside handlers.
var logger *slog.Logger

// initLogger configures the global logger based on the current environment.
// Called from main() right after godotenv.Load.
func initLogger() {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" || appEnv() == "development" {
		level = slog.LevelDebug
	}

	var handler slog.Handler
	switch appEnv() {
	case "production", "staging":
		// JSON in shared environments — easy to query in aggregators or grep
		// with jq. Add the env tag to every line so multi-tenant logs are
		// easy to filter.
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	default:
		// Text in dev — readable in a terminal.
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}

	logger = slog.New(handler).With("env", appEnv())
	slog.SetDefault(logger)
}

// logFromCtx returns the global logger pre-populated with the request ID
// from the context (if present). Use this in handlers so every log line
// inside a request can be correlated to the same X-Request-Id header that
// the client sees.
//
//	func handleFoo(db *DB) http.HandlerFunc {
//	    return func(w http.ResponseWriter, r *http.Request) {
//	        l := logFromCtx(r.Context())
//	        l.Info("doing the thing", "user", c.Email)
//	    }
//	}
func logFromCtx(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return logger
	}
	if reqID, ok := ctx.Value(reqIDKey).(string); ok && reqID != "" {
		return logger.With("request_id", reqID)
	}
	return logger
}

// logFromReq is a tiny shortcut so handlers can write `logFromReq(r).Info(...)`
// without grabbing the context first.
func logFromReq(r *http.Request) *slog.Logger {
	return logFromCtx(r.Context())
}

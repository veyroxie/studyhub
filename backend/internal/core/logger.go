package core

import (
	"context"
	"log/slog"
	"net/http"
	"os"
)

// Logger is the package-level structured Logger. Initialised by initLogger()
// in main(). Production uses JSON output (greppable, ships well to log
// aggregators); development uses human-readable text.
//
// Named `Logger` (not `log`) so it doesn't shadow Go's stdlib `log` package
// — main.go still uses log.Fatalf for startup errors, which is fine.
//
// Use it like:
//
//	Logger.Info("invoice paid", "invoice_id", id, "amount", amount)
//	Logger.Error("could not save invoice", "err", err, "student_id", sid)
//
// For request-scoped logging that includes the request_id automatically,
// use logFromCtx(ctx) inside handlers.
// Logger is never nil: InitDB and the boot-time migrations log before main()
// reaches InitLogger in some entry points (tests, tools), and a nil Logger
// turns a logged error into a panic. InitLogger replaces this default.
var Logger = slog.Default()

// initLogger configures the global Logger based on the current environment.
// Called from main() right after godotenv.Load.
func InitLogger() {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" || AppEnv() == "development" {
		level = slog.LevelDebug
	}

	var handler slog.Handler
	switch AppEnv() {
	case "production", "staging":
		// JSON in shared environments — easy to query in aggregators or grep
		// with jq. Add the env tag to every line so multi-tenant logs are
		// easy to filter.
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	default:
		// Text in dev — readable in a terminal.
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}

	Logger = slog.New(handler).With("env", AppEnv())
	slog.SetDefault(Logger)
}

// logFromCtx returns the global Logger pre-populated with the request ID
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
func LogFromCtx(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return Logger
	}
	if reqID, ok := ctx.Value(reqIDKey).(string); ok && reqID != "" {
		return Logger.With("request_id", reqID)
	}
	return Logger
}

// logFromReq is a tiny shortcut so handlers can write `logFromReq(r).Info(...)`
// without grabbing the context first.
func LogFromReq(r *http.Request) *slog.Logger {
	return LogFromCtx(r.Context())
}

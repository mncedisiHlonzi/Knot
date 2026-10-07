// Package httpapi is the HTTP surface of the Knot backend.
//
// It translates HTTP requests into calls on the identity service and translates
// the results back into JSON. It contains no business rules: validation,
// authentication decisions, and token issuance all live in internal/identity.
//
// Routing uses the standard library only. The Go 1.22 ServeMux method patterns
// ("GET /health") are used deliberately, so no router dependency is needed.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// requestIDHeader is the request-correlation header Knot reads and echoes.
const requestIDHeader = "X-Request-ID"

// requestIDKey is the context key under which the request id is stored. It is a
// private struct type so no other package can collide with it.
type requestIDKey struct{}

// RequestIDFromContext returns the request id stored by the request-id
// middleware, or "" when the middleware did not run.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// withRequestID ensures every request carries an id: the inbound X-Request-ID
// when present, a generated one otherwise. The id is echoed in the response
// header and stored on the context for logging.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = newRequestID()
		}

		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// statusRecorder captures the response status so the logging middleware can
// report it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader records the status before delegating.
func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Write records an implicit 200 if the handler never called WriteHeader.
func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// withRequestLogging logs one line per request, including the request id, method,
// path, status, and duration. It never logs bodies, headers, tokens, or DSNs.
func withRequestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		logger.InfoContext(
			r.Context(),
			"http request",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", recorder.status),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
	})
}

// withRecover turns a panic in a handler into a logged 500 rather than a dropped
// connection. It sits inside the logging middleware so the 500 is still logged.
func withRecover(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(
					r.Context(),
					"panic recovered",
					slog.String("request_id", RequestIDFromContext(r.Context())),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Any("panic", recovered),
					slog.String("stack", string(debug.Stack())),
				)
				writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// newRequestID returns a 128-bit random hex identifier.
func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is not a reason to drop a request; fall back to a
		// timestamp so the log line still correlates something.
		return fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// Package httpapi is the HTTP surface of the Knot backend.
//
// It translates HTTP requests into calls on the identity and stories services
// and translates the results back into JSON. It contains no business rules:
// validation, authentication decisions, and token issuance all live in
// internal/identity and internal/stories.
//
// Routing uses the standard library only. The Go 1.22 ServeMux method patterns
// ("GET /health") are used deliberately, so no router dependency is needed.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
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

// statusRecorder captures the response status and whether the body actually
// reached the client.
//
// The write error matters as much as the status. A handler declares its status in
// WriteHeader, which returns no error, so a body that fails afterwards — the
// classic case being a request that outlives the server's WriteTimeout — would
// otherwise be logged with the status the handler intended rather than the one the
// client experienced (KNOT-037-fix3).
type statusRecorder struct {
	http.ResponseWriter
	status int
	// declaredLength is the Content-Length the handler set before committing a
	// status, or -1 when it set none (a chunked or automatically-sized body).
	declaredLength int64
	// written counts the bytes the handler successfully handed to the writer.
	written  int64
	writeErr error
}

// WriteHeader records the status before delegating.
func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.declaredLength = contentLength(r.Header())
	r.ResponseWriter.WriteHeader(status)
}

// Write records an implicit 200 if the handler never called WriteHeader, and keeps
// the first write error so the middleware can report a response the client never
// received.
func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
		r.declaredLength = contentLength(r.Header())
	}

	n, err := r.ResponseWriter.Write(b)
	r.written += int64(n)
	if err != nil && r.writeErr == nil {
		r.writeErr = err
	}

	return n, err
}

// truncated reports whether the handler promised more bytes than it delivered.
//
// This catches the failure that leaves no write error behind: when the client
// hangs up mid-response, the copy usually dies on the *read* side first (the
// upstream body is cancelled), so `Write` never returns an error even though the
// client received a fraction of the body (KNOT-037-fix3).
func (r *statusRecorder) truncated(method string) bool {
	if r.declaredLength < 0 || method == http.MethodHead {
		return false
	}
	// These carry no body by definition, so a Content-Length on them is not a
	// promise about bytes to write.
	if r.status == http.StatusNoContent || r.status == http.StatusNotModified {
		return false
	}

	return r.written < r.declaredLength
}

// contentLength parses the response's declared Content-Length, or -1 when it is
// absent or unparseable.
func contentLength(header http.Header) int64 {
	raw := header.Get("Content-Length")
	if raw == "" {
		return -1
	}

	length, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || length < 0 {
		return -1
	}

	return length
}

// Flush keeps the wrapped writer flushable, which it would otherwise stop being: a
// streaming handler that asserts http.Flusher on the ResponseWriter would fail with
// a wrapper that does not implement it. A flush failure is recorded exactly like a
// write failure, because it means bytes did not reach the client either.
func (r *statusRecorder) Flush() {
	err := http.NewResponseController(r.ResponseWriter).Flush()
	if err == nil || errors.Is(err, http.ErrNotSupported) {
		return
	}
	if r.writeErr == nil {
		r.writeErr = err
	}
}

// Unwrap exposes the wrapped writer to http.ResponseController, so a handler can
// still reach SetWriteDeadline or Hijack through this wrapper.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// The three log messages a failed delivery can produce. The distinction is worth
// keeping when reading logs: a deadline means this server gave up on the client,
// any other write error means the connection broke beneath it, and a truncated body
// means the client left before the promise in Content-Length was honoured.
const (
	writeDeadlineMessage  = "write deadline exceeded; client did not receive response"
	writeFailedMessage    = "response write failed; client did not receive the full response"
	writeTruncatedMessage = "response body was not fully written; the client disconnected before it finished"
)

// isTimeoutWrite reports whether err means a deadline elapsed rather than a
// connection that failed for some other reason.
func isTimeoutWrite(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// deliveryFailure returns the message to log when this response did not reach the
// client, plus a detail string, or an empty message when it did.
//
// There are two ways a response can fail to arrive and they do not produce the same
// evidence: a write error (the deadline expired, or the connection broke), or a
// body that was promised and only partly delivered, which may leave no write error
// at all.
func (r *statusRecorder) deliveryFailure(method string) (message, detail string) {
	if r.writeErr != nil {
		if isTimeoutWrite(r.writeErr) {
			return writeDeadlineMessage, r.writeErr.Error()
		}
		return writeFailedMessage, r.writeErr.Error()
	}

	if r.truncated(method) {
		return writeTruncatedMessage, fmt.Sprintf("%d of %d bytes delivered", r.written, r.declaredLength)
	}

	return "", ""
}

// withRequestLogging logs one line per request, including the request id, method,
// path, status, and duration. It never logs bodies, headers, tokens, or DSNs.
//
// When the response body could not be written to the client, the line is an ERROR
// carrying the status the client experienced (503, the server could not deliver)
// rather than the status the handler intended, with that intended status kept as a
// separate field so the code path that ran is still visible. Reporting 201 for a
// response that never arrived is worse than reporting nothing: it sends whoever is
// debugging a slow upload looking in the wrong place (KNOT-037-fix3).
func withRequestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		// Flush before deciding what to log. A response body that fits in the
		// server's 2 KiB buffer is only written to the socket when the handler
		// returns, and net/http discards the error from that final flush — so
		// without this, a response the client never received looks perfectly
		// healthy to every layer above the socket. Flushing here is what makes a
		// missed deadline observable at all (KNOT-037-fix3).
		recorder.Flush()

		duration := time.Since(start).Milliseconds()
		requestID := RequestIDFromContext(r.Context())

		if message, detail := recorder.deliveryFailure(r.Method); message != "" {
			attrs := []slog.Attr{
				slog.String("request_id", requestID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", http.StatusServiceUnavailable),
				slog.Int("intended_status", recorder.status),
				slog.Bool("deadline_exceeded", isTimeoutWrite(recorder.writeErr)),
				slog.Int64("bytes_written", recorder.written),
				slog.Int64("bytes_declared", recorder.declaredLength),
				slog.Int64("duration_ms", duration),
			}
			if detail != "" {
				attrs = append(attrs, slog.String("error", detail))
			}

			logger.LogAttrs(r.Context(), slog.LevelError, message, attrs...)
			return
		}

		logger.InfoContext(
			r.Context(),
			"http request",
			slog.String("request_id", requestID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", recorder.status),
			slog.Int64("duration_ms", duration),
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

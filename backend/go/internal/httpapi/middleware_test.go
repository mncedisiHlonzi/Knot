package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer that can be written by a server goroutine and read
// by the test without a data race.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// logLines decodes the JSON log lines written so far.
func (b *syncBuffer) logLines(t *testing.T) []map[string]any {
	t.Helper()

	lines := []map[string]any{}
	for _, raw := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			t.Fatalf("log line is not JSON: %v (%q)", err, raw)
		}
		lines = append(lines, entry)
	}

	return lines
}

// failingWriter is a ResponseWriter whose body writes fail, standing in for a
// connection whose write deadline has already passed. WriteHeader succeeds and
// records the status, exactly as the real one does — which is the whole reason the
// defect existed: the status is committed before the body is known to be
// deliverable.
type failingWriter struct {
	header http.Header
	status int
	err    error
}

func (w *failingWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *failingWriter) WriteHeader(status int) { w.status = status }

func (w *failingWriter) Write([]byte) (int, error) { return 0, w.err }

// TestRequestLoggingReportsADeliveredFailureInsteadOfTheIntendedStatus is the
// regression test for the misleading media log (KNOT-037-fix3).
//
// It drives the real writeJSON, which is what the media handler calls: a
// WriteHeader(201) that succeeds, followed by a body write that fails. Before the
// fix the middleware logged `status=201` for that request; a reader debugging a
// slow upload would then look for a success that never reached the client.
func TestRequestLoggingReportsADeliveredFailureInsteadOfTheIntendedStatus(t *testing.T) {
	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The exact call the media handler makes on success.
		writeJSON(w, http.StatusCreated, map[string]any{"media": map[string]any{"id": "abc"}})
	})

	writer := &failingWriter{err: os.ErrDeadlineExceeded}
	request := httptest.NewRequest(http.MethodPost, "/stories/abc/media", nil)
	withRequestLogging(logger, handler).ServeHTTP(writer, request)

	lines := logs.logLines(t)
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want exactly 1: %v", len(lines), lines)
	}

	entry := lines[0]
	if got := entry["level"]; got != "ERROR" {
		t.Errorf("level = %v, want ERROR", got)
	}
	if got := entry["msg"]; got != writeDeadlineMessage {
		t.Errorf("msg = %v, want %q", got, writeDeadlineMessage)
	}
	if got := entry["status"]; got != float64(http.StatusServiceUnavailable) {
		t.Errorf("status = %v, want %d (what the client experienced)", got, http.StatusServiceUnavailable)
	}
	if got := entry["intended_status"]; got != float64(http.StatusCreated) {
		t.Errorf("intended_status = %v, want %d (the handler's own status, kept for diagnosis)", got, http.StatusCreated)
	}
	if got := entry["deadline_exceeded"]; got != true {
		t.Errorf("deadline_exceeded = %v, want true", got)
	}
	if got := entry["path"]; got != "/stories/abc/media" {
		t.Errorf("path = %v, want /stories/abc/media", got)
	}
}

// TestRequestLoggingReportsANonDeadlineWriteFailure pins that a broken connection
// is also reported as a failure — it must not be silently logged as a success just
// because the error is not a timeout.
func TestRequestLoggingReportsANonDeadlineWriteFailure(t *testing.T) {
	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("body the client never saw"))
	})

	writer := &failingWriter{err: errBrokenPipe}
	withRequestLogging(logger, handler).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/x", nil))

	lines := logs.logLines(t)
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want exactly 1: %v", len(lines), lines)
	}

	entry := lines[0]
	if got := entry["level"]; got != "ERROR" {
		t.Errorf("level = %v, want ERROR", got)
	}
	if got := entry["msg"]; got != writeFailedMessage {
		t.Errorf("msg = %v, want %q", got, writeFailedMessage)
	}
	if got := entry["status"]; got != float64(http.StatusServiceUnavailable) {
		t.Errorf("status = %v, want %d", got, http.StatusServiceUnavailable)
	}
	if got := entry["deadline_exceeded"]; got != false {
		t.Errorf("deadline_exceeded = %v, want false", got)
	}
}

// TestRequestLoggingKeepsTheSuccessLineUnchanged is the guard against the fix
// being too eager: a delivered response must still log one INFO line with the
// handler's status.
func TestRequestLoggingKeepsTheSuccessLineUnchanged(t *testing.T) {
	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
	})

	recorder := httptest.NewRecorder()
	withRequestLogging(logger, handler).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/stories/abc/media", nil))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("response status = %d, want %d", recorder.Code, http.StatusCreated)
	}

	lines := logs.logLines(t)
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want exactly 1: %v", len(lines), lines)
	}

	entry := lines[0]
	if got := entry["level"]; got != "INFO" {
		t.Errorf("level = %v, want INFO", got)
	}
	if got := entry["msg"]; got != "http request" {
		t.Errorf("msg = %v, want %q", got, "http request")
	}
	if got := entry["status"]; got != float64(http.StatusCreated) {
		t.Errorf("status = %v, want %d", got, http.StatusCreated)
	}
	if _, present := entry["intended_status"]; present {
		t.Error("a delivered response must not carry intended_status")
	}
}

// TestRequestLoggingCatchesARealWriteTimeout exercises the actual mechanism rather
// than a substitute: a real HTTP server with a short WriteTimeout, and a handler
// that finishes after it. This is the case that produced a `status=201` line for a
// response the client never received.
func TestRequestLoggingCatchesARealWriteTimeout(t *testing.T) {
	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	const (
		writeTimeout = 50 * time.Millisecond
		handlerDelay = 250 * time.Millisecond
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Stands in for the R2 upload that outlived the deadline.
		time.Sleep(handlerDelay)
		writeJSON(w, http.StatusCreated, map[string]any{"media": map[string]any{"id": "abc"}})
	})

	server := httptest.NewUnstartedServer(withRequestLogging(logger, handler))
	server.Config.WriteTimeout = writeTimeout
	server.Start()
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Post(server.URL+"/stories/abc/media", "application/json", bytes.NewReader([]byte("{}")))
	if err == nil {
		_ = response.Body.Close()
	}

	// The middleware logs after the handler returns, which is a moment after the
	// client gives up, so poll rather than assume ordering.
	entry := waitForField(t, &logs, "level", "ERROR")
	if got := entry["status"]; got != float64(http.StatusServiceUnavailable) {
		t.Errorf("status = %v, want %d — a response the client never received must not be logged as success", got, http.StatusServiceUnavailable)
	}
	if got := entry["intended_status"]; got != float64(http.StatusCreated) {
		t.Errorf("intended_status = %v, want %d", got, http.StatusCreated)
	}
	if got := entry["deadline_exceeded"]; got != true {
		t.Errorf("deadline_exceeded = %v, want true (a real WriteTimeout elapsed)", got)
	}

	// And the misleading line must be gone entirely.
	for _, line := range logs.logLines(t) {
		if line["msg"] == "http request" && line["status"] == float64(http.StatusCreated) {
			t.Fatalf("a request that outlived WriteTimeout was still logged as 201: %v", line)
		}
	}
}

// TestRequestLoggingReportsATruncatedBody covers the failure that leaves no write
// error behind: a handler that promised a Content-Length and delivered less. This
// is what a client hanging up mid-download looks like from the server's side — the
// copy dies on the upstream read, so every Write succeeded.
func TestRequestLoggingReportsATruncatedBody(t *testing.T) {
	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		// The client went away, so the upstream copy stopped here.
		_, _ = w.Write([]byte("0123456789"))
	})

	recorder := httptest.NewRecorder()
	withRequestLogging(logger, handler).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/stories/x/media/y/content", nil))

	lines := logs.logLines(t)
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want exactly 1: %v", len(lines), lines)
	}

	entry := lines[0]
	if got := entry["level"]; got != "ERROR" {
		t.Errorf("level = %v, want ERROR", got)
	}
	if got := entry["msg"]; got != writeTruncatedMessage {
		t.Errorf("msg = %v, want %q", got, writeTruncatedMessage)
	}
	if got := entry["status"]; got != float64(http.StatusServiceUnavailable) {
		t.Errorf("status = %v, want %d", got, http.StatusServiceUnavailable)
	}
	if got := entry["intended_status"]; got != float64(http.StatusOK) {
		t.Errorf("intended_status = %v, want %d", got, http.StatusOK)
	}
	if got := entry["bytes_written"]; got != float64(10) {
		t.Errorf("bytes_written = %v, want 10", got)
	}
	if got := entry["bytes_declared"]; got != float64(100) {
		t.Errorf("bytes_declared = %v, want 100", got)
	}
}

// TestRequestLoggingDoesNotFlagAHeadResponseWithContentLength guards the truncation
// check against a false positive: a HEAD response declares the body's length and
// correctly sends none of it.
func TestRequestLoggingDoesNotFlagAHeadResponseWithContentLength(t *testing.T) {
	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "36426574")
		w.WriteHeader(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	withRequestLogging(logger, handler).ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/stories/x/media/y/content", nil))

	entry := logs.logLines(t)[0]
	if got := entry["level"]; got != "INFO" {
		t.Errorf("level = %v, want INFO — a HEAD response is not a truncated body: %v", got, entry)
	}
	if got := entry["status"]; got != float64(http.StatusOK) {
		t.Errorf("status = %v, want %d", got, http.StatusOK)
	}
}

// errBrokenPipe stands in for a connection reset by the peer. It is deliberately
// not a timeout, so the middleware must report it as a write failure rather than a
// missed deadline.
var errBrokenPipe = errors.New("write tcp 127.0.0.1:1234->127.0.0.1:5678: write: broken pipe")

// waitForField polls the log buffer until a line whose field equals want appears.
func waitForField(t *testing.T, logs *syncBuffer, field, want string) map[string]any {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range logs.logLines(t) {
			if line[field] == want {
				return line
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("no log line with %s=%q; got: %s", field, want, logs.String())
	return nil
}

package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// writeDecodeError reports a request body that could not be read or parsed.
//
// It is shared by every handler that accepts a JSON body so that all of them
// answer an unreadable body identically: 413 for a body over the size cap, 400
// for anything else. The underlying parse error is logged but never returned to
// the client, because json.Decoder messages quote the input and would echo
// request content back.
func writeDecodeError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, codeRequestTooLarge,
			fmt.Sprintf("request body must be at most %d bytes", tooLarge.Limit))
		return
	}

	logger.WarnContext(
		r.Context(),
		"rejected request body",
		slog.String("request_id", RequestIDFromContext(r.Context())),
		slog.String("path", r.URL.Path),
		slog.String("error", err.Error()),
	)
	writeError(w, http.StatusBadRequest, codeInvalidRequest, "request body must be a single valid JSON object with no unknown fields")
}

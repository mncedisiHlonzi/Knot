package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Media writes and the story detail cache (KNOT-036-fix4).
//
// An upload or a delete changes the media array a story detail returns, so the
// story's cached payload goes with it. The recorder is the same fake detail cache
// the KNOT-036-fix tests use, so these assertions are about the keys the handler
// deletes rather than about a mock's internals.

// uploadMediaThroughRouter publishes one image through the real route table.
func uploadMediaThroughRouter(t *testing.T, env *storyMediaTestEnv, router http.Handler) storyMediaResponse {
	t.Helper()
	return env.uploadMedia(t, router, pngImage(t), "camera")
}

func TestStoryMediaUploadInvalidatesTheStoryDetail(t *testing.T) {
	details := newDetailCacheRecorder()
	env := newStoryMediaTestEnvWith(t, details, discardLogger())
	router := env.router(t, testMediaAuthorID)

	uploadMediaThroughRouter(t, env, router)

	state := details.snapshot()
	if len(state.invalidatedStories) != 1 || state.invalidatedStories[0] != testMediaStoryID {
		t.Fatalf("invalidated stories = %v, want [%s]", state.invalidatedStories, testMediaStoryID)
	}
	// A version detail inherits the story's media, but an upload cannot name every
	// adaptation, so no version key is touched (KNOT-036-fix4 non-goal).
	if len(state.invalidatedVersions) != 0 {
		t.Errorf("invalidated versions = %v, want none", state.invalidatedVersions)
	}
}

func TestStoryMediaDeleteInvalidatesTheStoryDetail(t *testing.T) {
	details := newDetailCacheRecorder()
	env := newStoryMediaTestEnvWith(t, details, discardLogger())
	router := env.router(t, testMediaAuthorID)

	created := uploadMediaThroughRouter(t, env, router)

	request := httptest.NewRequest(http.MethodDelete, "/stories/"+testMediaStoryID+"/media/"+created.ID, nil)
	request.Header.Set("Authorization", "Bearer "+testAccessToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}

	// Two writes happened — the upload and the delete — and each invalidates the
	// story's detail. The LAST one is what matters: the detail is stale again after
	// the delete.
	state := details.snapshot()
	if len(state.invalidatedStories) != 2 {
		t.Fatalf("invalidated stories = %v, want two calls (upload, delete)", state.invalidatedStories)
	}
	for i, storyID := range state.invalidatedStories {
		if storyID != testMediaStoryID {
			t.Errorf("invalidation %d = %q, want %q", i, storyID, testMediaStoryID)
		}
	}
}

// TestStoryMediaUploadSucceedsWhenInvalidationFails is the best-effort contract: a
// Redis failure is a logged warning, never a failed upload.
func TestStoryMediaUploadSucceedsWhenInvalidationFails(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	details := newDetailCacheRecorder()
	details.invalidateErr = errCacheDown

	env := newStoryMediaTestEnvWith(t, details, logger)
	router := env.router(t, testMediaAuthorID)

	created := uploadMediaThroughRouter(t, env, router)
	if created.ID == "" {
		t.Fatal("upload returned no media id; a failed invalidation must not fail the upload")
	}

	output := logs.String()
	if !strings.Contains(output, "level=WARN") {
		t.Errorf("logs = %q, want a WARN line", output)
	}
	if !strings.Contains(output, "story detail cache invalidation failed") {
		t.Errorf("logs = %q, want the invalidation warning", output)
	}
}

// TestStoryMediaDeleteSucceedsWhenInvalidationFails is the delete half of the same
// contract.
func TestStoryMediaDeleteSucceedsWhenInvalidationFails(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	details := newDetailCacheRecorder()
	env := newStoryMediaTestEnvWith(t, details, logger)
	router := env.router(t, testMediaAuthorID)

	created := uploadMediaThroughRouter(t, env, router)

	// Arm the failure only for the delete, so the log assertion is about it.
	details.invalidateErr = errCacheDown

	request := httptest.NewRequest(http.MethodDelete, "/stories/"+testMediaStoryID+"/media/"+created.ID, nil)
	request.Header.Set("Authorization", "Bearer "+testAccessToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: a failed invalidation must not fail the delete",
			recorder.Code, http.StatusNoContent)
	}
	if !strings.Contains(logs.String(), "story detail cache invalidation failed") {
		t.Errorf("logs = %q, want the invalidation warning", logs.String())
	}
}

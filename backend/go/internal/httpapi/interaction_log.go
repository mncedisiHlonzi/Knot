package httpapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/knot/backend/internal/interactions"
)

// InteractionRecorder records one user's engagement with a story or version.
//
// It is the HTTP layer's view of `interactions.Service`. Handlers depend on this
// one-method interface rather than the concrete service, so their tests need no
// database (KNOT-026).
type InteractionRecorder interface {
	Log(ctx context.Context, userID, entityType, entityID, interactionType string) error
}

// interactionLogTimeout bounds one background write. It is deliberately short:
// the log is enrichment for a future ranking pass, so a slow database must not
// leave goroutines piling up behind a busy server.
const interactionLogTimeout = 5 * time.Second

// recordInteraction appends one row to the interaction log without delaying or
// failing the request that produced it.
//
// This is the non-blocking boundary the relevance engine's data pipeline depends
// on: the write runs on its own goroutine, against a context detached from the
// request (a handler's context is cancelled the moment it returns), and bounded
// by `interactionLogTimeout`. A failure is logged at Warn and dropped — the user
// already completed the action, and their response must not depend on telemetry.
//
// A nil recorder (no wiring), a signed-out reader (no user id), or a caller that
// could not resolve an entity id is a silent no-op.
func recordInteraction(
	ctx context.Context,
	logger *slog.Logger,
	recorder InteractionRecorder,
	userID string,
	entityType string,
	entityID string,
	interactionType string,
) {
	if recorder == nil || userID == "" || entityID == "" {
		return
	}

	detached := context.WithoutCancel(ctx)
	go func() {
		writeCtx, cancel := context.WithTimeout(detached, interactionLogTimeout)
		defer cancel()

		if err := recorder.Log(writeCtx, userID, entityType, entityID, interactionType); err != nil {
			logger.Warn(
				"interaction log dropped",
				"interaction", interactionType,
				"entity_type", entityType,
				"entity_id", entityID,
				"error", err,
			)
		}
	}()
}

// The interaction-type values the handlers record, re-exported from the domain
// so a call site cannot invent a spelling.
const (
	interactionView     = string(interactions.InteractionView)
	interactionReaction = string(interactions.InteractionReaction)
	interactionComment  = string(interactions.InteractionComment)
	interactionAdapt    = string(interactions.InteractionAdapt)
)

// The entity types the log accepts, re-exported for the same reason.
const (
	interactionEntityStory   = interactions.EntityStory
	interactionEntityVersion = interactions.EntityVersion
)

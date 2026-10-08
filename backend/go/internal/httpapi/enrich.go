package httpapi

import (
	"context"
	"log/slog"

	"github.com/knot/backend/internal/rooted"
)

// RootedLookup is the enrichment dependency: one batched read of the primary
// public Rooted signal for a set of authors.
//
// It is deliberately narrower than the whole Rooted service. The stories,
// versions, and conversations handlers only need to decorate a response with an
// author's inline Rooted summary, so they depend on the single method that does
// that rather than on signal writing and reading.
type RootedLookup interface {
	BatchGetPrimaryPublicSignals(ctx context.Context, userIDs []string) (map[string]*rooted.Signal, error)
}

// rootedSummary is the inline Rooted projection attached to content responses.
//
// It carries only what an inline badge needs. The signal's id, its owner, and its
// timestamps are deliberately absent: the enrichment is a display hint, not a
// lookup of the full signal, so nothing that identifies the owner beyond their
// already-public author id leaves the building here (KNOT-ADR-017).
type rootedSummary struct {
	Place          string                `json:"place"`
	DurationBucket rooted.DurationBucket `json:"duration_bucket"`
}

// authorRootedSummaries loads the primary public Rooted signal for every distinct
// author id in one call and returns a map from author id to the inline summary.
//
// Every response that carries authored entities goes through here once, so the
// enrichment is a single batch query per response rather than one query per row.
//
// Enrichment is supplementary, so a failure is logged and swallowed rather than
// failing the whole response: a problem reading Rooted must not make stories,
// versions, comments, or bridges unreadable. A user with no public signal simply
// has no entry, and the response carries null for them.
func authorRootedSummaries(ctx context.Context, lookup RootedLookup, logger *slog.Logger, authorIDs []string) map[string]*rootedSummary {
	unique := uniqueAuthorIDs(authorIDs)
	if len(unique) == 0 {
		return nil
	}

	signals, err := lookup.BatchGetPrimaryPublicSignals(ctx, unique)
	if err != nil {
		logger.WarnContext(
			ctx,
			"rooted enrichment failed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil
	}

	if len(signals) == 0 {
		return nil
	}

	summaries := make(map[string]*rootedSummary, len(signals))
	for authorID, signal := range signals {
		if signal == nil {
			continue
		}
		summaries[authorID] = &rootedSummary{
			Place:          signal.Place,
			DurationBucket: signal.DurationBucket,
		}
	}

	return summaries
}

// uniqueAuthorIDs returns the distinct, non-empty ids in the order they first
// appear, so the batch query carries each author once.
func uniqueAuthorIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))

	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}

	return out
}

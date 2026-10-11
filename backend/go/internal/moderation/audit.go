package moderation

import (
	"context"
	"fmt"
	"log/slog"
)

// AuditStore is the append-only moderation trail. Every block and every report
// writes one entry; 017b's moderator actions will write one each too.
type AuditStore interface {
	// Log appends one audit entry. targetID may be "" for an action that names no
	// single entity, in which case it is stored as NULL. metadata may be nil; it
	// is stored as an empty JSON object then.
	Log(ctx context.Context, actorID, action, targetType, targetID string, metadata map[string]any) error
}

// Auditor records a moderation event. It is the seam the block and report
// services depend on, so they never touch the store directly.
//
// A write is best-effort: the implementation logs a failure and returns, so a
// problem with the audit trail never fails the user's action, exactly as a
// notification failure never fails the content that triggered it (KNOT-ADR-038).
type Auditor interface {
	// Log records one event. targetID may be "" for an action that names no single
	// entity; metadata may be nil.
	Log(ctx context.Context, actorID, action, targetType, targetID string, metadata map[string]any)
}

// AuditService is the Auditor implementation backed by an AuditStore.
type AuditService struct {
	store  AuditStore
	logger *slog.Logger
}

// NewAuditService wires an audit store and a logger into the audit trail.
func NewAuditService(store AuditStore, logger *slog.Logger) (*AuditService, error) {
	if store == nil {
		return nil, fmt.Errorf("moderation: audit service requires an audit store")
	}
	if logger == nil {
		return nil, fmt.Errorf("moderation: audit service requires a logger")
	}
	return &AuditService{store: store, logger: logger}, nil
}

// Log records one event, swallowing and logging a store failure.
func (s *AuditService) Log(ctx context.Context, actorID, action, targetType, targetID string, metadata map[string]any) {
	if err := s.store.Log(ctx, actorID, action, targetType, targetID, metadata); err != nil {
		s.logger.WarnContext(ctx,
			"audit write failed",
			slog.String("action", action),
			slog.String("actor_id", actorID),
			slog.String("error", err.Error()),
		)
	}
}

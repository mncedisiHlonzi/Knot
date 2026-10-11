package moderation

import "log/slog"

// Service is the composed moderation service the composition root wires and the
// HTTP layer depends on. It embeds the block and report services, so a single
// value offers Block, Unblock, ListBlocks, IsBlocked, BlockedPairs,
// BlockedUserIDs, CreateReport, ListMyReports, and UpsertCaseForEntity.
//
// The auth middleware needs only BlockedUserIDs; the moderation handler needs the
// rest. Bundling them means one constructor and one dependency to thread.
type Service struct {
	*BlockService
	*ReportService
}

// NewService wires the block and report services over one store.
func NewService(store *PostgresStore, entities EntityLookup, logger *slog.Logger) (*Service, error) {
	audit, err := NewAuditService(store, logger)
	if err != nil {
		return nil, err
	}

	blocks, err := NewBlockService(store, audit)
	if err != nil {
		return nil, err
	}

	reports, err := NewReportService(store, entities, audit)
	if err != nil {
		return nil, err
	}

	return &Service{BlockService: blocks, ReportService: reports}, nil
}

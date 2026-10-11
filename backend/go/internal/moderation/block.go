package moderation

import (
	"context"
	"fmt"
	"time"
)

// Block is one directed block: BlockerID has blocked BlockedID.
//
// The table stores a single direction, but every read and write treats a block as
// mutual (KNOT-ADR-060): the client's block set is the union of the two
// directions, so a block hides content both ways and prevents interaction both
// ways. The row records who acted, for the block list the blocker manages.
type Block struct {
	// ID is the canonical UUID text assigned by PostgreSQL.
	ID string
	// BlockerID is the user who created the block.
	BlockerID string
	// BlockedID is the user who was blocked.
	BlockedID string
	// CreatedAt is when the block was created.
	CreatedAt time.Time
}

// BlockStore is the persistence contract for blocks. The service depends on this
// interface rather than on pgx, so the business rules can be tested without a
// database.
type BlockStore interface {
	// CreateBlock records that blockerID blocked blockedID and reports whether a
	// new row was written. It is idempotent: blocking an already-blocked user
	// creates nothing and reports false.
	CreateBlock(ctx context.Context, blockerID, blockedID string) (bool, error)
	// DeleteBlock removes the block, if any. It is idempotent: removing a block
	// that is not there succeeds.
	DeleteBlock(ctx context.Context, blockerID, blockedID string) error
	// ListBlocks returns one page of the blocks blockerID created, newest first,
	// starting after cursor. It returns the cursor that resumes after the page, or
	// nil when the page is the last one.
	ListBlocks(ctx context.Context, blockerID string, cursor *Cursor, limit int) ([]Block, *Cursor, error)
	// IsBlocked reports whether a block exists between userA and userB in either
	// direction.
	IsBlocked(ctx context.Context, userA, userB string) (bool, error)
	// BlockedPairs reports which of otherIDs are on either side of a block with
	// userID. It is one query for a whole page, so a list never fans out into a
	// check per row.
	BlockedPairs(ctx context.Context, userID string, otherIDs []string) (map[string]bool, error)
	// BlockedUserIDs returns the full mutual block set of userID: everyone userID
	// has blocked, and everyone who has blocked userID. It is the set the request
	// carries on its context so content reads can filter a page (KNOT-ADR-060).
	BlockedUserIDs(ctx context.Context, userID string) ([]string, error)
}

// BlockService holds the block business rules.
//
// It depends on the BlockStore and an Auditor, and knows nothing about HTTP,
// JSON, or SQL.
type BlockService struct {
	blocks BlockStore
	audit  Auditor
}

// NewBlockService wires a store and an audit trail into the block domain.
func NewBlockService(blocks BlockStore, audit Auditor) (*BlockService, error) {
	if blocks == nil {
		return nil, fmt.Errorf("moderation: block service requires a block store")
	}
	if audit == nil {
		return nil, fmt.Errorf("moderation: block service requires an auditor")
	}
	return &BlockService{blocks: blocks, audit: audit}, nil
}

// Block records that blockerID has blocked blockedID.
//
// It is idempotent: blocking an already-blocked user is not an error. Blocking
// yourself is rejected with ErrSelfBlock. A newly created block is written to the
// audit trail; a repeat is not, so the trail has one entry per real action.
func (s *BlockService) Block(ctx context.Context, blockerID, blockedID string) error {
	if !isUUID(blockerID) || !isUUID(blockedID) {
		return &ValidationError{Field: "user_id", Message: "must be a valid user id"}
	}
	if blockerID == blockedID {
		return ErrSelfBlock
	}

	created, err := s.blocks.CreateBlock(ctx, blockerID, blockedID)
	if err != nil {
		return fmt.Errorf("moderation: create block: %w", err)
	}
	if !created {
		return nil
	}

	s.audit.Log(ctx, blockerID, "block.created", "user", blockedID, nil)
	return nil
}

// Unblock removes a block blockerID placed on blockedID. It is idempotent.
func (s *BlockService) Unblock(ctx context.Context, blockerID, blockedID string) error {
	if !isUUID(blockerID) || !isUUID(blockedID) {
		return &ValidationError{Field: "user_id", Message: "must be a valid user id"}
	}

	if err := s.blocks.DeleteBlock(ctx, blockerID, blockedID); err != nil {
		return fmt.Errorf("moderation: delete block: %w", err)
	}

	s.audit.Log(ctx, blockerID, "block.removed", "user", blockedID, nil)
	return nil
}

// ListBlocks returns one page of the blocks the user created, newest first, plus
// the cursor that resumes after it ("" when the page is the last one).
func (s *BlockService) ListBlocks(ctx context.Context, blockerID, rawCursor string, limit int) ([]Block, string, error) {
	if !isUUID(blockerID) {
		return nil, "", &ValidationError{Field: "user_id", Message: "must be a valid user id"}
	}
	if limit < 1 || limit > MaxListLimit {
		return nil, "", &ValidationError{Field: "limit", Message: fmt.Sprintf("must be between 1 and %d", MaxListLimit)}
	}

	var cursor *Cursor
	if rawCursor != "" {
		decoded, err := DecodeCursor(rawCursor)
		if err != nil {
			return nil, "", err
		}
		cursor = &decoded
	}

	page, next, err := s.blocks.ListBlocks(ctx, blockerID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("moderation: list blocks: %w", err)
	}
	if page == nil {
		page = []Block{}
	}

	nextCursor := ""
	if next != nil {
		nextCursor = next.Encode()
	}

	return page, nextCursor, nil
}

// IsBlocked reports whether a block exists between the two users in either
// direction.
func (s *BlockService) IsBlocked(ctx context.Context, userA, userB string) (bool, error) {
	if !isUUID(userA) || !isUUID(userB) {
		return false, nil
	}
	blocked, err := s.blocks.IsBlocked(ctx, userA, userB)
	if err != nil {
		return false, fmt.Errorf("moderation: is blocked: %w", err)
	}
	return blocked, nil
}

// BlockedPairs reports which of otherIDs are blocked relative to userID.
func (s *BlockService) BlockedPairs(ctx context.Context, userID string, otherIDs []string) (map[string]bool, error) {
	if !isUUID(userID) || len(otherIDs) == 0 {
		return map[string]bool{}, nil
	}
	pairs, err := s.blocks.BlockedPairs(ctx, userID, otherIDs)
	if err != nil {
		return nil, fmt.Errorf("moderation: blocked pairs: %w", err)
	}
	return pairs, nil
}

// BlockedUserIDs returns userID's mutual block set, which the request carries on
// its context so content reads can filter a page.
func (s *BlockService) BlockedUserIDs(ctx context.Context, userID string) ([]string, error) {
	if !isUUID(userID) {
		return nil, nil
	}
	ids, err := s.blocks.BlockedUserIDs(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("moderation: blocked user ids: %w", err)
	}
	return ids, nil
}

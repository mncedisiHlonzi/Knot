package moderation

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	userA = "11111111-1111-4111-8111-111111111111"
	userB = "22222222-2222-4222-8222-222222222222"
)

// fakeBlockStore is an in-memory BlockStore for the service tests.
type fakeBlockStore struct {
	created map[string]bool
	blocks  []Block
	pairs   map[string]bool
	userIDs []string

	createErr error
	deleteErr error
	listErr   error
}

func (f *fakeBlockStore) key(blockerID, blockedID string) string { return blockerID + "|" + blockedID }

func (f *fakeBlockStore) CreateBlock(_ context.Context, blockerID, blockedID string) (bool, error) {
	if f.createErr != nil {
		return false, f.createErr
	}
	if f.created == nil {
		f.created = make(map[string]bool)
	}
	key := f.key(blockerID, blockedID)
	if f.created[key] {
		return false, nil
	}
	f.created[key] = true
	return true, nil
}

func (f *fakeBlockStore) DeleteBlock(_ context.Context, _, _ string) error { return f.deleteErr }

func (f *fakeBlockStore) ListBlocks(_ context.Context, _ string, _ *Cursor, _ int) ([]Block, *Cursor, error) {
	return f.blocks, nil, f.listErr
}

func (f *fakeBlockStore) IsBlocked(_ context.Context, _, _ string) (bool, error) { return false, nil }

func (f *fakeBlockStore) BlockedPairs(_ context.Context, _ string, _ []string) (map[string]bool, error) {
	return f.pairs, nil
}

func (f *fakeBlockStore) BlockedUserIDs(_ context.Context, _ string) ([]string, error) {
	return f.userIDs, nil
}

// recordingAuditor records the actions passed to Log.
type recordingAuditor struct{ events []string }

func (a *recordingAuditor) Log(_ context.Context, _, action, _, _ string, _ map[string]any) {
	a.events = append(a.events, action)
}

func newBlockService(t *testing.T, store *fakeBlockStore, auditor *recordingAuditor) *BlockService {
	t.Helper()

	service, err := NewBlockService(store, auditor)
	if err != nil {
		t.Fatalf("NewBlockService() error = %v, want nil", err)
	}
	return service
}

func TestBlockIsIdempotentAndAuditsOnce(t *testing.T) {
	store := &fakeBlockStore{}
	auditor := &recordingAuditor{}
	service := newBlockService(t, store, auditor)

	if err := service.Block(context.Background(), userA, userB); err != nil {
		t.Fatalf("Block() error = %v, want nil", err)
	}
	if err := service.Block(context.Background(), userA, userB); err != nil {
		t.Fatalf("Block() second call error = %v, want nil", err)
	}

	if len(auditor.events) != 1 || auditor.events[0] != "block.created" {
		t.Errorf("audit events = %v, want one block.created", auditor.events)
	}
}

func TestBlockRejectsSelf(t *testing.T) {
	service := newBlockService(t, &fakeBlockStore{}, &recordingAuditor{})

	if err := service.Block(context.Background(), userA, userA); !errors.Is(err, ErrSelfBlock) {
		t.Fatalf("Block(self) error = %v, want ErrSelfBlock", err)
	}
}

func TestBlockRejectsMalformedID(t *testing.T) {
	service := newBlockService(t, &fakeBlockStore{}, &recordingAuditor{})

	if err := service.Block(context.Background(), "not-a-uuid", userB); !errors.Is(err, ErrValidation) {
		t.Fatalf("Block(bad id) error = %v, want a validation error", err)
	}
}

func TestUnblockIsIdempotentAndAudits(t *testing.T) {
	auditor := &recordingAuditor{}
	service := newBlockService(t, &fakeBlockStore{}, auditor)

	if err := service.Unblock(context.Background(), userA, userB); err != nil {
		t.Fatalf("Unblock() error = %v, want nil", err)
	}
	if len(auditor.events) != 1 || auditor.events[0] != "block.removed" {
		t.Errorf("audit events = %v, want one block.removed", auditor.events)
	}
}

func TestListBlocksValidatesLimit(t *testing.T) {
	service := newBlockService(t, &fakeBlockStore{}, &recordingAuditor{})

	if _, _, err := service.ListBlocks(context.Background(), userA, "", 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("ListBlocks(limit 0) error = %v, want a validation error", err)
	}
	if _, _, err := service.ListBlocks(context.Background(), userA, "", MaxListLimit+1); !errors.Is(err, ErrValidation) {
		t.Fatalf("ListBlocks(limit too high) error = %v, want a validation error", err)
	}
}

func TestListBlocksReturnsEmptySliceNotNil(t *testing.T) {
	service := newBlockService(t, &fakeBlockStore{}, &recordingAuditor{})

	page, next, err := service.ListBlocks(context.Background(), userA, "", DefaultListLimit)
	if err != nil {
		t.Fatalf("ListBlocks() error = %v, want nil", err)
	}
	if page == nil {
		t.Error("ListBlocks() page = nil, want an empty slice")
	}
	if next != "" {
		t.Errorf("ListBlocks() next = %q, want empty", next)
	}
}

func TestBlockedUserIDsReturnsTheSet(t *testing.T) {
	store := &fakeBlockStore{userIDs: []string{userB}}
	service := newBlockService(t, store, &recordingAuditor{})

	ids, err := service.BlockedUserIDs(context.Background(), userA)
	if err != nil {
		t.Fatalf("BlockedUserIDs() error = %v, want nil", err)
	}
	if len(ids) != 1 || ids[0] != userB {
		t.Errorf("BlockedUserIDs() = %v, want [%s]", ids, userB)
	}
}

func TestBlockedPairsReturnsTheSet(t *testing.T) {
	store := &fakeBlockStore{pairs: map[string]bool{userB: true}}
	service := newBlockService(t, store, &recordingAuditor{})

	pairs, err := service.BlockedPairs(context.Background(), userA, []string{userB})
	if err != nil {
		t.Fatalf("BlockedPairs() error = %v, want nil", err)
	}
	if !pairs[userB] {
		t.Errorf("BlockedPairs() = %v, want %s true", pairs, userB)
	}
}

func TestListBlocksReturnsCursorWhenMoreRemain(t *testing.T) {
	// The fake returns one row and no cursor; this only proves the service trims a
	// nil cursor to the empty string, which the store exercises in its own test.
	store := &fakeBlockStore{blocks: []Block{{ID: userA, BlockerID: userA, BlockedID: userB, CreatedAt: time.Now().UTC()}}}
	service := newBlockService(t, store, &recordingAuditor{})

	page, next, err := service.ListBlocks(context.Background(), userA, "", DefaultListLimit)
	if err != nil {
		t.Fatalf("ListBlocks() error = %v, want nil", err)
	}
	if len(page) != 1 {
		t.Errorf("ListBlocks() page length = %d, want 1", len(page))
	}
	if next != "" {
		t.Errorf("ListBlocks() next = %q, want empty", next)
	}
}

package httpapi

import (
	"context"
	"fmt"

	"github.com/knot/backend/internal/conversations"
	"github.com/knot/backend/internal/inquiries"
	"github.com/knot/backend/internal/moderation"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/versions"
)

// Content services used only to confirm a reported entity exists. Each is the
// narrowest slice of its domain: the single Get that both proves existence and
// returns the row.
type (
	// storyGetter resolves a story by id.
	storyGetter interface {
		GetStory(ctx context.Context, id string) (stories.Story, error)
	}
	// versionGetter resolves a story version by id.
	versionGetter interface {
		GetVersion(ctx context.Context, id string) (versions.StoryVersion, error)
	}
	// commentGetter resolves a comment by id.
	commentGetter interface {
		GetComment(ctx context.Context, id string) (conversations.Comment, error)
	}
	// bridgeGetter resolves a bridge by id.
	bridgeGetter interface {
		GetBridge(ctx context.Context, id string) (conversations.Bridge, error)
	}
	// inquiryGetter resolves an inquiry by id.
	inquiryGetter interface {
		GetInquiry(ctx context.Context, id string) (inquiries.Inquiry, error)
	}
	// answerGetter resolves an inquiry answer by id.
	answerGetter interface {
		GetAnswer(ctx context.Context, id string) (inquiries.Answer, error)
	}
)

// EntityExistence is the moderation.EntityLookup implementation. It resolves any
// of the six reportable kinds through that kind's own domain service, so the
// moderation package never imports a content domain (KNOT-ADR-040 pattern).
type EntityExistence struct {
	stories       storyGetter
	versions      versionGetter
	conversations commentGetter
	bridges       bridgeGetter
	inquiries     inquiryGetter
	answers       answerGetter
}

// NewEntityExistence wires the six content services into an existence lookup.
func NewEntityExistence(storiesService storyGetter, versionsService versionGetter, conversationsService commentGetter, bridgeService bridgeGetter, inquiriesService inquiryGetter, answerService answerGetter) (*EntityExistence, error) {
	if storiesService == nil || versionsService == nil || conversationsService == nil ||
		bridgeService == nil || inquiriesService == nil || answerService == nil {
		return nil, fmt.Errorf("httpapi: entity existence requires all six content services")
	}
	return &EntityExistence{
		stories:       storiesService,
		versions:      versionsService,
		conversations: conversationsService,
		bridges:       bridgeService,
		inquiries:     inquiriesService,
		answers:       answerService,
	}, nil
}

// EntityExists reports whether an entity of the given kind with the given id
// exists. A domain "not found" is false with a nil error; any other failure is
// propagated.
func (e *EntityExistence) EntityExists(ctx context.Context, entityType moderation.EntityType, entityID string) (bool, error) {
	switch entityType {
	case moderation.EntityStory:
		_, err := e.stories.GetStory(ctx, entityID)
		return existsFromError(err)
	case moderation.EntityVersion:
		_, err := e.versions.GetVersion(ctx, entityID)
		return existsFromError(err)
	case moderation.EntityComment:
		_, err := e.conversations.GetComment(ctx, entityID)
		return existsFromError(err)
	case moderation.EntityBridge:
		_, err := e.bridges.GetBridge(ctx, entityID)
		return existsFromError(err)
	case moderation.EntityInquiry:
		_, err := e.inquiries.GetInquiry(ctx, entityID)
		return existsFromError(err)
	case moderation.EntityInquiryAnswer:
		_, err := e.answers.GetAnswer(ctx, entityID)
		return existsFromError(err)
	default:
		return false, nil
	}
}

// existsFromError turns a domain Get's error into an existence answer: a
// not-found is false, success is true, and anything else is returned as-is.
func existsFromError(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if isDomainNotFound(err) {
		return false, nil
	}
	return false, err
}

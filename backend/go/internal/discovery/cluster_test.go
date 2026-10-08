package discovery

import (
	"errors"
	"testing"
	"time"

	"github.com/knot/backend/internal/stories"
)

// TestValidationErrorUnwrapsSentinel proves a ValidationError is both a concrete
// type (for the handler's field/message extraction) and ErrValidation (for a
// generic errors.Is test), which is the contract every domain in Knot keeps.
func TestValidationErrorUnwrapsSentinel(t *testing.T) {
	err := error(&ValidationError{Field: "pillar", Message: "must be one of wonder, heritage"})

	if !errors.Is(err, ErrValidation) {
		t.Error("errors.Is(err, ErrValidation) = false, want true")
	}

	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatal("errors.As(err, *ValidationError) = false, want true")
	}
	if validation.Field != "pillar" {
		t.Errorf("field = %q, want %q", validation.Field, "pillar")
	}
	if err.Error() == "" {
		t.Error("Error() = empty, want a message")
	}
}

// TestLimitsAreSane pins the documented bounds so a careless edit to a constant
// is caught here rather than at a request boundary.
func TestLimitsAreSane(t *testing.T) {
	if DefaultClusterLimit != 100 {
		t.Errorf("DefaultClusterLimit = %d, want 100", DefaultClusterLimit)
	}
	if MaxClusterLimit != 500 {
		t.Errorf("MaxClusterLimit = %d, want 500", MaxClusterLimit)
	}
	if DefaultPlaceLimit != 20 {
		t.Errorf("DefaultPlaceLimit = %d, want 20", DefaultPlaceLimit)
	}
	if MaxPlaceLimit != 50 {
		t.Errorf("MaxPlaceLimit = %d, want 50", MaxPlaceLimit)
	}

	if DefaultClusterLimit < 1 || DefaultClusterLimit > MaxClusterLimit {
		t.Errorf("cluster default %d must fall within 1..%d", DefaultClusterLimit, MaxClusterLimit)
	}
	if DefaultPlaceLimit < 1 || DefaultPlaceLimit > MaxPlaceLimit {
		t.Errorf("place default %d must fall within 1..%d", DefaultPlaceLimit, MaxPlaceLimit)
	}
}

// TestPlaceClusterCarriesTheAggregate checks the value type holds every field the
// wire shape needs, including both pillar keys.
func TestPlaceClusterCarriesTheAggregate(t *testing.T) {
	latest := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

	cluster := PlaceCluster{
		Place:      "Cape Town",
		StoryCount: 3,
		PillarCounts: map[stories.Pillar]int{
			stories.PillarWonder:   2,
			stories.PillarHeritage: 1,
		},
		Languages:     []string{"af", "en"},
		LatestStoryAt: latest,
	}

	if cluster.Place != "Cape Town" {
		t.Errorf("place = %q, want %q", cluster.Place, "Cape Town")
	}
	if cluster.StoryCount != 3 {
		t.Errorf("story count = %d, want 3", cluster.StoryCount)
	}
	if cluster.PillarCounts[stories.PillarWonder] != 2 || cluster.PillarCounts[stories.PillarHeritage] != 1 {
		t.Errorf("pillar counts = %v, want wonder:2 heritage:1", cluster.PillarCounts)
	}
	if len(cluster.Languages) != 2 {
		t.Errorf("languages = %v, want two entries", cluster.Languages)
	}
	if !cluster.LatestStoryAt.Equal(latest) {
		t.Errorf("latest = %v, want %v", cluster.LatestStoryAt, latest)
	}
}

// TestClusterFilterZeroValueIsUnfiltered documents that the zero filter is valid:
// an empty pillar and language mean "do not filter".
func TestClusterFilterZeroValueIsUnfiltered(t *testing.T) {
	var filter ClusterFilter

	if filter.Pillar != "" {
		t.Errorf("pillar = %q, want empty", filter.Pillar)
	}
	if filter.Language != "" {
		t.Errorf("language = %q, want empty", filter.Language)
	}
	if filter.Limit != 0 {
		t.Errorf("limit = %d, want 0", filter.Limit)
	}
}

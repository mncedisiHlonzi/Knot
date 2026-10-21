package relevance

import (
	"context"
	"testing"
	"time"
)

// fixedNow is the clock every score below is measured against.
var fixedNow = time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)

// newTestRanker builds a ranker with a fixed clock, so a score is reproducible.
func newTestRanker(profile UserProfile, engagement EngagementSummary) *SimpleRanker {
	return NewSimpleRanker(profile, engagement, WithNow(func() time.Time { return fixedNow }))
}

// candidate builds a story candidate created `age` before the fixed clock.
func candidate(id string, age time.Duration) StoryCandidate {
	return StoryCandidate{
		StoryID:   id,
		AuthorID:  "someone-else",
		Language:  "eng",
		Pillar:    "wonder",
		Place:     "Oaxaca",
		CreatedAt: fixedNow.Add(-age),
	}
}

func TestSimpleRankerScoresTheLadder(t *testing.T) {
	profile := UserProfile{PreferredLanguages: []string{"zul"}}
	engagement := EngagementSummary{WeightedByPillar: map[string]float64{"heritage": 42}}

	cases := []struct {
		name      string
		candidate StoryCandidate
		reader    string
		want      float64
	}{
		{
			name:      "base score alone",
			candidate: candidate("base", 0),
			reader:    "reader-1",
			want:      BaseScore,
		},
		{
			name:      "own content",
			candidate: func() StoryCandidate { c := candidate("own", 0); c.AuthorID = "reader-1"; return c }(),
			reader:    "reader-1",
			want:      BaseScore + OwnContentBonus,
		},
		{
			name:      "preferred language",
			candidate: func() StoryCandidate { c := candidate("lang", 0); c.Language = "zul"; return c }(),
			reader:    "reader-1",
			want:      BaseScore + LanguageBonus,
		},
		{
			name:      "pillar affinity",
			candidate: func() StoryCandidate { c := candidate("pillar", 0); c.Pillar = "heritage"; return c }(),
			reader:    "reader-1",
			want:      BaseScore + PillarBonus,
		},
		{
			name:      "two hours old",
			candidate: candidate("old", 2*time.Hour),
			reader:    "reader-1",
			want:      BaseScore - 2*RecencyDecayPerHour,
		},
		{
			name: "every signal at once",
			candidate: func() StoryCandidate {
				c := candidate("all", 2*time.Hour)
				c.AuthorID = "reader-1"
				c.Language = "zul"
				c.Pillar = "heritage"
				return c
			}(),
			reader: "reader-1",
			want:   BaseScore + OwnContentBonus + LanguageBonus + PillarBonus - 2*RecencyDecayPerHour,
		},
	}

	ranker := newTestRanker(profile, engagement)
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ranked, err := ranker.Rank(context.Background(), test.reader, []StoryCandidate{test.candidate})
			if err != nil {
				t.Fatalf("Rank() error = %v, want nil", err)
			}
			if len(ranked) != 1 {
				t.Fatalf("Rank() returned %d candidates, want 1", len(ranked))
			}
			if ranked[0].Score != test.want {
				t.Errorf("Score = %v, want %v", ranked[0].Score, test.want)
			}
		})
	}
}

func TestSimpleRankerDoesNotRewardAFutureTimestamp(t *testing.T) {
	ranker := newTestRanker(UserProfile{}, EngagementSummary{})
	future := candidate("future", -3*time.Hour)

	ranked, err := ranker.Rank(context.Background(), "reader-1", []StoryCandidate{future})
	if err != nil {
		t.Fatalf("Rank() error = %v, want nil", err)
	}
	if ranked[0].Score != BaseScore {
		t.Errorf("Score = %v, want %v (decay clamps at zero)", ranked[0].Score, BaseScore)
	}
}

func TestSimpleRankerSkipsPillarAffinityWithoutEngagement(t *testing.T) {
	ranker := newTestRanker(UserProfile{}, EngagementSummary{})

	ranked, err := ranker.Rank(context.Background(), "reader-1", []StoryCandidate{
		candidate("heritage-story", 0),
	})
	if err != nil {
		t.Fatalf("Rank() error = %v, want nil", err)
	}
	if ranked[0].Score != BaseScore {
		t.Errorf("Score = %v, want %v (no engagement, no bonus)", ranked[0].Score, BaseScore)
	}
}

func TestTopPillarIsDeterministicOnATie(t *testing.T) {
	summary := EngagementSummary{WeightedByPillar: map[string]float64{
		"wonder":   12,
		"heritage": 12,
	}}

	if got := summary.TopPillar(); got != "heritage" {
		t.Errorf("TopPillar() = %q, want %q (alphabetical tie-break)", got, "heritage")
	}
}

func TestTopPillarIgnoresBlankAndNonPositiveWeights(t *testing.T) {
	summary := EngagementSummary{WeightedByPillar: map[string]float64{
		"":         99,
		"wonder":   0,
		"heritage": -3,
	}}

	if got := summary.TopPillar(); got != "" {
		t.Errorf("TopPillar() = %q, want empty", got)
	}
}

func TestSimpleRankerOrdersDeterministically(t *testing.T) {
	profile := UserProfile{PreferredLanguages: []string{"zul"}}

	// Two candidates match the language and lead by score; between them the
	// fresher one must win. Two more tie on score and timestamp, so the lower
	// story id must break the tie. One older candidate trails on decay alone.
	preferredNew := candidate("p-new", time.Hour)
	preferredNew.Language = "zul"
	preferredOld := candidate("p-old", 2*time.Hour)
	preferredOld.Language = "zul"
	tieA := candidate("a-tie", 0)
	tieB := candidate("b-tie", 0)
	older := candidate("c-old", time.Hour)

	ranker := newTestRanker(profile, EngagementSummary{})
	ranked, err := ranker.Rank(context.Background(), "reader-1", []StoryCandidate{
		older, preferredOld, tieB, preferredNew, tieA,
	})
	if err != nil {
		t.Fatalf("Rank() error = %v, want nil", err)
	}

	want := []string{"p-new", "p-old", "a-tie", "b-tie", "c-old"}
	for index, id := range want {
		if ranked[index].StoryID != id {
			t.Fatalf("ranked[%d].StoryID = %q, want %q (order: %v)", index, ranked[index].StoryID, id, storyIDs(ranked))
		}
	}
}

func TestSimpleRankerDoesNotMutateItsInput(t *testing.T) {
	candidates := []StoryCandidate{candidate("second", 2*time.Hour), candidate("first", time.Hour)}

	ranker := newTestRanker(UserProfile{}, EngagementSummary{})
	if _, err := ranker.Rank(context.Background(), "reader-1", candidates); err != nil {
		t.Fatalf("Rank() error = %v, want nil", err)
	}

	if candidates[0].StoryID != "second" || candidates[1].StoryID != "first" {
		t.Errorf("Rank() reordered its input slice: [%s %s]", candidates[0].StoryID, candidates[1].StoryID)
	}
}

func TestSimpleRankerKeepsVersionIdentity(t *testing.T) {
	version := candidate("story-1", time.Hour)
	version.VersionID = "version-2"

	ranker := newTestRanker(UserProfile{}, EngagementSummary{})
	ranked, err := ranker.Rank(context.Background(), "reader-1", []StoryCandidate{version})
	if err != nil {
		t.Fatalf("Rank() error = %v, want nil", err)
	}

	if ranked[0].StoryID != "story-1" || ranked[0].VersionID != "version-2" {
		t.Errorf("RankedStory = %+v, want story-1/version-2", ranked[0].StoryCandidate)
	}
}

// storyIDs is a compact view of a ranking for failure messages.
func storyIDs(ranked []RankedStory) []string {
	ids := make([]string, 0, len(ranked))
	for _, item := range ranked {
		ids = append(ids, item.StoryID)
	}
	return ids
}

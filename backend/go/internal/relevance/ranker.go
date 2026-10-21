// Package relevance ranks candidate stories for one reader.
//
// It is the pluggable half of Knot's relevance engine (KNOT-026): the `Ranker`
// interface is the seam a future machine-learning model plugs into, and
// `SimpleRanker` is today's deterministic, explainable implementation.
//
// Nothing here touches a database or the network. A caller fetches the reader's
// profile and their engagement summary (from `user_interactions`, joined to
// `entity_features`) and injects them; `Rank` is then a pure function of its
// arguments and its injected clock. That is what makes the ranking testable
// without a PostgreSQL instance and reviewable by a human.
//
// The scoring ladder is deliberately legible — a base score, three bonuses, and
// a linear recency decay — because at this stage the value is in the data
// foundation and the seam, not in a tuned model.
package relevance

import (
	"context"
	"sort"
	"strings"
	"time"
)

// The scoring ladder. They are exported so a report, a test, or a future
// explainability surface can name the same numbers the ranker applies.
const (
	// BaseScore is what every candidate starts with, so a score is never zero.
	BaseScore float64 = 1
	// OwnContentBonus is added when the reader wrote the candidate. Seeing your
	// own telling again is useful, not an accident.
	OwnContentBonus float64 = 100
	// LanguageBonus is added when the candidate is in one of the reader's
	// preferred languages: the platform's core match.
	LanguageBonus float64 = 50
	// PillarBonus is added when the candidate shares the pillar the reader has
	// engaged with most.
	PillarBonus float64 = 20
	// RecencyDecayPerHour is subtracted for every hour since the candidate was
	// created, so older content sinks without disappearing.
	RecencyDecayPerHour float64 = 1
)

// StoryCandidate is one entry the ranker may order.
//
// A candidate is a story or a version: `StoryID` is always set, `VersionID`
// only when the candidate is an adaptation. The fields are the ones the engine
// needs — identity, authorship, language, pillar, place, and age — not the full
// story payload.
type StoryCandidate struct {
	// StoryID is the story the candidate belongs to.
	StoryID string
	// VersionID is the adaptation's id, or empty for a story entry.
	VersionID string
	// AuthorID is who wrote this telling.
	AuthorID string
	// Language is the candidate's ISO 639-3 code.
	Language string
	// Pillar is the story's pillar.
	Pillar string
	// Place is the story's approximate place, for future place affinity. The
	// current ladder does not score it.
	Place string
	// CreatedAt is when this telling was written; the recency decay's input.
	CreatedAt time.Time
}

// RankedStory is a candidate with the score the ranker gave it.
type RankedStory struct {
	StoryCandidate
	// Score is the sum of the ladder's components. It is deterministic for a
	// given candidate, profile, engagement summary, and clock.
	Score float64
}

// UserProfile is the slice of the reader's profile the ranker needs.
type UserProfile struct {
	// PreferredLanguages is the reader's canonical ISO 639-3 list, in order.
	PreferredLanguages []string
}

// EngagementSummary is the reader's logged engagement, pre-aggregated by the
// caller.
//
// It is a read model, not a query: `PillarEngagement` in internal/interactions
// produces it from `user_interactions` joined to `entity_features`.
type EngagementSummary struct {
	// WeightedByPillar is the total interaction weight the reader has spent on
	// content in each pillar.
	WeightedByPillar map[string]float64
}

// TopPillar answers the pillar the reader has engaged with most.
//
// Ties are broken alphabetically so the answer is deterministic regardless of
// map iteration order. A blank pillar, or a summary with no engagement, answers
// the empty string, which the ranker reads as "no affinity".
func (s EngagementSummary) TopPillar() string {
	top := ""
	topWeight := 0.0

	for pillar, weight := range s.WeightedByPillar {
		candidate := strings.TrimSpace(pillar)
		if candidate == "" || weight <= 0 {
			continue
		}
		if weight > topWeight || (weight == topWeight && (top == "" || candidate < top)) {
			top = candidate
			topWeight = weight
		}
	}

	return top
}

// Ranker orders candidates for one reader.
//
// It is the extension point: an ML-backed implementation can satisfy the same
// interface without changing a caller. `userID` identifies the reader whose
// ranking is requested.
type Ranker interface {
	Rank(ctx context.Context, userID string, candidates []StoryCandidate) ([]RankedStory, error)
}

// Option customises a SimpleRanker.
type Option func(*SimpleRanker)

// WithNow injects the clock the recency decay measures against. Tests use a
// fixed instant so a score is reproducible; production leaves the default
// (`time.Now`).
func WithNow(now func() time.Time) Option {
	return func(ranker *SimpleRanker) {
		if now != nil {
			ranker.now = now
		}
	}
}

// SimpleRanker is the deterministic, rule-based Ranker.
//
// Its dependencies are injected at construction because they belong to the
// caller's request, not to the algorithm:
//
//   - the reader's preferred languages, for the language match;
//   - the reader's engagement summary, for the pillar affinity.
//
// `Rank`'s `userID` is the reader being ranked for; it is used for the
// own-content rule. Construction is cheap, so a caller builds one per request.
type SimpleRanker struct {
	languages  map[string]struct{}
	engagement EngagementSummary
	topPillar  string
	now        func() time.Time
}

// NewSimpleRanker returns a ranker for one reader's profile and engagement.
func NewSimpleRanker(profile UserProfile, engagement EngagementSummary, options ...Option) *SimpleRanker {
	languages := make(map[string]struct{}, len(profile.PreferredLanguages))
	for _, language := range profile.PreferredLanguages {
		trimmed := strings.TrimSpace(language)
		if trimmed != "" {
			languages[trimmed] = struct{}{}
		}
	}

	ranker := &SimpleRanker{
		languages:  languages,
		engagement: engagement,
		topPillar:  engagement.TopPillar(),
		now:        time.Now,
	}
	for _, option := range options {
		option(ranker)
	}

	return ranker
}

// Rank scores every candidate and returns them best first.
//
// The ladder, in order:
//
//  1. every candidate starts at BaseScore;
//  2. +OwnContentBonus when the candidate's author is the reader;
//  3. +LanguageBonus when the candidate's language is one of the reader's
//     preferred languages;
//  4. +PillarBonus when the candidate shares the reader's most-engaged pillar
//     (skipped when the reader has no engagement yet);
//  5. −RecencyDecayPerHour for every hour since the candidate was created. A
//     candidate with a future timestamp is not rewarded for it: decay is
//     clamped at zero.
//
// The order is total and deterministic: score descending, then newest first,
// then story id, then version id. The input slice is never mutated.
func (r *SimpleRanker) Rank(ctx context.Context, userID string, candidates []StoryCandidate) ([]RankedStory, error) {
	_ = ctx
	reader := strings.TrimSpace(userID)
	now := r.now()

	ranked := make([]RankedStory, 0, len(candidates))
	for _, candidate := range candidates {
		score := BaseScore

		if reader != "" && strings.TrimSpace(candidate.AuthorID) == reader {
			score += OwnContentBonus
		}

		language := strings.TrimSpace(candidate.Language)
		if _, preferred := r.languages[language]; preferred {
			score += LanguageBonus
		}

		if r.topPillar != "" && strings.TrimSpace(candidate.Pillar) == r.topPillar {
			score += PillarBonus
		}

		if hours := now.Sub(candidate.CreatedAt).Hours(); hours > 0 {
			score -= hours * RecencyDecayPerHour
		}

		ranked = append(ranked, RankedStory{StoryCandidate: candidate, Score: score})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		if !ranked[i].CreatedAt.Equal(ranked[j].CreatedAt) {
			return ranked[i].CreatedAt.After(ranked[j].CreatedAt)
		}
		if ranked[i].StoryID != ranked[j].StoryID {
			return ranked[i].StoryID < ranked[j].StoryID
		}
		return ranked[i].VersionID < ranked[j].VersionID
	})

	return ranked, nil
}

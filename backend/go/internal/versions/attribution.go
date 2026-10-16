package versions

import (
	"context"
	"errors"
	"fmt"

	"github.com/knot/backend/internal/language"
)

// ErrChainCycle is returned when a version's parent pointers form a cycle, so a
// walk to the root never terminates. The schema's CHECK constraint stops a
// version from being its own parent, but it cannot stop a longer loop (A adapts
// B, B adapts A), so the walk guards against one rather than trusting the data.
var ErrChainCycle = errors.New("versions: attribution chain contains a cycle")

// maxChainDepth bounds a walk even when the cycle guard somehow misses, so a
// malformed tree can never hang a request. A real chain is a handful of links
// long; this is a ceiling, not a rule.
const maxChainDepth = 512

// The wording a link falls back to when its author or language could not be
// resolved. Attribution is supplementary, so a missing name must read as prose
// rather than as an empty gap in the sentence (KNOT-021).
const (
	unknownAuthor   = "another author"
	unknownLanguage = "their language"
)

// ChainLink is one hop in a version's lineage: the version itself, who wrote it,
// and what language they wrote it in.
//
// AuthorDisplayName is deliberately left empty by BuildChain and filled by the
// caller from its own batched author read, so resolving a chain never becomes a
// query per link. FormatAttribution falls back to a generic phrase when it is
// empty.
type ChainLink struct {
	// VersionID is the version at this hop.
	VersionID string
	// StoryID is the story every hop of a chain shares.
	StoryID string
	// AuthorID is the user who wrote this hop.
	AuthorID string
	// AuthorDisplayName is who to name in the sentence, or empty when unknown.
	AuthorDisplayName string
	// Language is the ISO 639-3 code this hop is written in, or empty when the
	// row somehow carries none.
	Language string
	// Depth is the hop's distance from the root: 0 for the story's original, 1
	// for the first adaptation, and so on.
	Depth int
}

// Chain is a version's lineage, ordered root-first, so the last link is the
// version the chain was built for.
type Chain []ChainLink

// Root returns the chain's first link — the story's original version — and
// whether the chain has one.
func (c Chain) Root() (ChainLink, bool) {
	if len(c) == 0 {
		return ChainLink{}, false
	}
	return c[0], true
}

// Target returns the chain's last link — the version the chain was built for —
// and whether the chain has one.
func (c Chain) Target() (ChainLink, bool) {
	if len(c) == 0 {
		return ChainLink{}, false
	}
	return c[len(c)-1], true
}

// Depth returns how many adaptations separate the target from the original: 0
// for the root version itself, 1 for a first adaptation, and so on. An empty
// chain reports 0 rather than -1, so a caller that could not resolve a chain
// never reports a nonsense depth.
func (c Chain) Depth() int {
	if len(c) == 0 {
		return 0
	}
	return len(c) - 1
}

// BuildChain walks up parent_version_id from versionID to the story's root and
// returns the lineage ordered root-first.
//
// It returns ErrNotFound when the version (or a version further up) does not
// exist, and ErrChainCycle when the parent pointers loop. Each hop is one store
// read; a caller that already holds a story's versions should use
// BuildChainFromVersions instead, which resolves the same chain without
// touching the store at all.
func BuildChain(ctx context.Context, store VersionStore, versionID string) (Chain, error) {
	if store == nil {
		return nil, fmt.Errorf("versions: build chain: nil store")
	}
	if !isUUID(versionID) {
		// An id that cannot name a version names nothing, which is not-found
		// rather than a malformed request (the GetVersion rule).
		return nil, ErrNotFound
	}

	return walkChain(versionID, func(id string) (StoryVersion, error) {
		return store.GetVersion(ctx, id)
	})
}

// BuildChainFromVersions resolves a chain entirely in memory from versions that
// have already been loaded, so a caller holding a story's whole version list
// pays no extra query per version.
//
// It is the batched counterpart of BuildChain: the tree endpoint loads every
// version once and then resolves each one's chain here, rather than walking the
// database once per row (KNOT-021).
func BuildChainFromVersions(targetID string, rows []StoryVersion) (Chain, error) {
	byID := make(map[string]StoryVersion, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}

	if _, ok := byID[targetID]; !ok {
		return nil, ErrNotFound
	}

	return walkChain(targetID, func(id string) (StoryVersion, error) {
		row, ok := byID[id]
		if !ok {
			return StoryVersion{}, ErrNotFound
		}
		return row, nil
	})
}

// walkChain is the shared walk. load supplies one version by id, either from the
// store or from an in-memory index, so both public entry points share one set of
// termination and cycle rules.
//
// It collects the links target-first and reverses them at the end, so the
// returned chain always reads root-first. A missing version is reported as
// ErrNotFound (wrapped, so errors.Is still matches), and a repeat is reported as
// ErrChainCycle rather than looping.
func walkChain(targetID string, load func(id string) (StoryVersion, error)) (Chain, error) {
	links := make([]ChainLink, 0, 4)
	seen := make(map[string]bool, 4)

	for id := targetID; ; {
		if seen[id] {
			return nil, fmt.Errorf("versions: walk chain from %s: %w", targetID, ErrChainCycle)
		}
		if len(seen) >= maxChainDepth {
			return nil, fmt.Errorf("versions: walk chain from %s: %w", targetID, ErrChainCycle)
		}
		seen[id] = true

		version, err := load(id)
		if err != nil {
			if id == targetID {
				// The version asked about does not exist, which is the
				// caller-visible not-found.
				return nil, err
			}
			return nil, fmt.Errorf("versions: walk chain from %s: parent %s: %w", targetID, id, err)
		}

		links = append(links, ChainLink{
			VersionID: version.ID,
			StoryID:   version.StoryID,
			AuthorID:  version.AuthorID,
			Language:  version.Language,
		})

		if version.IsRoot() {
			break
		}
		id = version.ParentVersionID
	}

	// Reverse into root-first order and stamp each link's depth from its
	// position, so the root is always link 0.
	chain := make(Chain, 0, len(links))
	for i := len(links) - 1; i >= 0; i-- {
		link := links[i]
		link.Depth = len(chain)
		chain = append(chain, link)
	}

	return chain, nil
}

// FormatAttribution renders a chain as the human sentence shown under a
// version's byline. It is a pure function of the chain: the caller fills each
// link's AuthorDisplayName beforehand, and a link that names no author or no
// language degrades to a generic phrase rather than leaving a hole.
//
// The four shapes, by chain length:
//
//	1     Original story by {author} in {language}.
//	2     Adapted from {root}'s original story in {root language} into {language}.
//	3     Adapted from {middle}'s {language} version of {root}'s original story
//	      in {root language} into {language}.
//	4+    Adapted from {parent}'s {language} version, which traces back through
//	      {N-2} adaptations to {root}'s original story in {root language}, into
//	      {language}.
//
// An empty chain renders as the empty string, which a caller reads as "no
// attribution to show".
func FormatAttribution(chain Chain) string {
	switch len(chain) {
	case 0:
		return ""
	case 1:
		root := chain[0]
		return fmt.Sprintf(
			"Original story by %s in %s.",
			displayName(root), displayLanguage(root),
		)
	case 2:
		root, target := chain[0], chain[1]
		return fmt.Sprintf(
			"Adapted from %s's original story in %s into %s.",
			displayName(root), displayLanguage(root), displayLanguage(target),
		)
	case 3:
		root, middle, target := chain[0], chain[1], chain[2]
		return fmt.Sprintf(
			"Adapted from %s's %s version of %s's original story in %s into %s.",
			displayName(middle), displayLanguage(middle),
			displayName(root), displayLanguage(root),
			displayLanguage(target),
		)
	default:
		root := chain[0]
		parent := chain[len(chain)-2]
		target := chain[len(chain)-1]
		return fmt.Sprintf(
			"Adapted from %s's %s version, which traces back through %d adaptations to %s's original story in %s, into %s.",
			displayName(parent), displayLanguage(parent),
			len(chain)-2,
			displayName(root), displayLanguage(root),
			displayLanguage(target),
		)
	}
}

// displayName names a link's author, falling back to a generic phrase when the
// name was never resolved (a deleted account, or a caller that skipped the
// batched author read).
func displayName(link ChainLink) string {
	if link.AuthorDisplayName == "" {
		return unknownAuthor
	}
	return link.AuthorDisplayName
}

// displayLanguage names a link's language, falling back to a generic phrase when
// the code is missing or is not one the canonical list knows.
func displayLanguage(link ChainLink) string {
	name, ok := language.Name(link.Language)
	if !ok || name == "" {
		return unknownLanguage
	}
	return name
}

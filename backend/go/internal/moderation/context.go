package moderation

import "context"

// excludedAuthorsKey is the context key under which the request's hidden-author
// set is stored. It is a private struct type so no other package can collide with
// it, matching the userIDContextKey convention in internal/httpapi.
type excludedAuthorsKey struct{}

// WithExcludedAuthors returns ctx carrying the set of author ids whose content
// must be hidden from the viewer of this request.
//
// The set is the viewer's *mutual* block set: everyone the viewer has blocked and
// everyone who has blocked the viewer. It is resolved once per authenticated
// request (in the auth middleware) and read by the content stores, so a list
// query filters a whole page with one predicate rather than a query per row
// (KNOT-ADR-060). Duplicate and empty ids are dropped, and the result is a fresh
// slice, so a caller cannot mutate the stored set.
func WithExcludedAuthors(ctx context.Context, ids []string) context.Context {
	if len(ids) == 0 {
		return ctx
	}

	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return ctx
	}

	return context.WithValue(ctx, excludedAuthorsKey{}, unique)
}

// ExcludedAuthors returns the viewer's hidden-author set, or nil when there is
// none.
//
// A nil result means "hide nothing": an anonymous reader has no blocks, and a
// reader with an empty block list has nothing to hide. The returned slice must
// not be mutated by the caller.
func ExcludedAuthors(ctx context.Context) []string {
	ids, ok := ctx.Value(excludedAuthorsKey{}).([]string)
	if !ok {
		return nil
	}
	return ids
}

// IsExcludedAuthor reports whether authorID is hidden from the viewer of ctx.
//
// It exists so a service can refuse a write that targets a blocked author's
// content without loading the whole set: the check is a linear scan over a small
// slice that is already on the context.
func IsExcludedAuthor(ctx context.Context, authorID string) bool {
	for _, id := range ExcludedAuthors(ctx) {
		if id == authorID {
			return true
		}
	}
	return false
}

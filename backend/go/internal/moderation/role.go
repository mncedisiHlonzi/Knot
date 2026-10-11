// Package moderation implements the Knot safety foundation: user roles, block
// lists, and content reports (KNOT-017a).
//
// It is part 1 of 2. It ships everything needed to *collect* moderation signals —
// who has blocked whom, and which content has been reported — but nothing that
// acts on them. The moderator queue, the hide/warn/suspend actions, and the audit
// endpoint ship in KNOT-017b. The schema for those (moderation_actions) is
// created here so 017b needs no second migration.
//
// The package is deliberately layered, mirroring internal/stories:
//
//	role.go            the Role type and its helpers
//	block.go           Block, BlockStore, BlockService
//	report.go          Report, ReportStore, EntityLookup, ReportService
//	audit.go           AuditStore (the append-only moderation trail)
//	cursor.go          the opaque (created_at, id) pagination cursor
//	context.go         the request-scoped excluded-author set
//	store.go           shared store helpers (scan, uuid, null mapping)
//	postgres_store.go  the PostgreSQL implementation of the store contracts
//	service.go         the composed Service the composition root wires
//
// Blocking is *mutual hiding plus write prevention* (KNOT-ADR-060). The table
// stores one directed row per block, but every read and every write consults both
// directions: if A blocked B, then neither A's content is shown to B nor B's to A,
// and neither can interact with the other's content. The request-scoped set of
// authors hidden from a viewer is resolved once per request and carried on the
// context (context.go), so the content stores can filter a page with one
// predicate and no fan-out.
//
// Nothing in this package knows about HTTP, JSON, or SQL types. Ids are plain
// strings holding canonical UUID text (KNOT-ADR-010).
package moderation

// Role is a user's moderation role. It is stored in the users.role column and is
// a closed set: 'user', 'moderator', or 'admin'.
//
// Roles are assigned manually at MVP; there is deliberately no API to grant one.
// The type is a plain string so a role can travel between the identity domain
// (which stores it) and this package (which interprets it) without either
// importing the other.
type Role string

// The three roles.
const (
	// RoleUser is an ordinary account. It is the default for every new user.
	RoleUser Role = "user"
	// RoleModerator may review the moderation queue and act on reports (017b).
	RoleModerator Role = "moderator"
	// RoleAdmin may do everything a moderator can, and manage the platform.
	RoleAdmin Role = "admin"
)

// Valid reports whether r is one of the three roles.
func (r Role) Valid() bool {
	switch r {
	case RoleUser, RoleModerator, RoleAdmin:
		return true
	default:
		return false
	}
}

// IsModerator reports whether a role may moderate: a moderator or an admin.
//
// It takes the stored string rather than the Role type so callers holding a raw
// users.role value (identity.User.Role is a string) do not have to convert first.
func IsModerator(role string) bool {
	return role == string(RoleModerator) || role == string(RoleAdmin)
}

// IsAdmin reports whether a role is the administrator role.
func IsAdmin(role string) bool {
	return role == string(RoleAdmin)
}

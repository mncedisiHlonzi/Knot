# Knot — Security Model

**Status: MVP, subject to change.** This document is the canonical description of how Knot
authenticates people and what protects their accounts. It is written to be read by whoever
is on call, so it states the rules, the numbers, and — where it matters more — the
trade-offs and their consequences.

Every number here is a constant in the Go code (`internal/auth`, `internal/identity`), and
a test pins them (`TestLimitRulesMatchTheDocumentedNumbers`), so this page cannot drift from
the implementation without failing CI.

Related: [KNOT-ADR-079](DECISIONS.md) through [KNOT-ADR-090](DECISIONS.md), and the
[API reference](API.md#post-authregister).

---

## 1. The account model

An account is four things:

| Table | Holds | Secret in it? |
| --- | --- | --- |
| `users` | The account itself, plus `email_verified_at`, `deleted_at`, `deletion_scheduled_at` | `password_hash` (deprecated, still what sign-in reads) |
| `credentials` | One row per sign-in method: `password`, `oauth_google`, `magic_link` | The method's opaque secret |
| `sessions` | One row per signed-in device | The **hash** of the refresh token |
| `verification_tokens` | Single-use, expiring proofs of address ownership | The **hash** of the token |

Secrets are always stored as argon2id hashes (passwords) or SHA-256 digests (tokens). No
token is ever recoverable from the database or from Redis.

### Consent

User consent is enforced at registration. The acceptance of Terms of Service and Privacy
Policy is recorded at the time of account creation: the API refuses a registration that
does not carry `agreed_to_terms: true`, and the created account is that record — there is
no separate acceptance table or timestamp (KNOT-035). Both documents are canonical in
[`PRIVACY_POLICY.md`](PRIVACY_POLICY.md) and [`TERMS_OF_SERVICE.md`](TERMS_OF_SERVICE.md),
and the app shows those same documents from the consent row on the Register screen.

## 2. Passwords

- Minimum **8 characters**, maximum **1024 bytes**, and at least **one letter and one
  number** (`identity.ValidatePassword`, KNOT-ADR-086).
- Enforced **server-side only**. The client may mirror the rule for feedback; it is never
  the authority.
- Hashed with **argon2id**, PHC-formatted, verified in constant time.
- **Timing equalisation**: a sign-in for an unknown address verifies against a dummy hash,
  so a failed attempt costs the same as a real one and cannot be used to enumerate accounts.
- The legacy `users.password_hash` column is **deprecated but not dropped**; it is still
  what `POST /auth/login` reads. `credentials` is its successor (KNOT-ADR-079), and the
  mirror is written on registration and on reset.

## 3. Sign-in, in order

1. Normalise the address (trim, lower-case).
2. Resolve the account through the **active** lookup, which excludes accounts inside their
   deletion window. Unknown and deleted are indistinguishable.
3. Verify the password (or burn equivalent work for an unknown address).
4. **Require `email_verified_at`.** An unverified account gets `403 email_not_verified`,
   only after the password check succeeded (KNOT-ADR-083).
5. Open a session: mint an opaque refresh token, store only its hash, and enforce the
   session cap.
6. Issue a 15-minute JWT access token whose `sid` claim names the session.

## 4. Tokens

| Token | Form | Lifetime | Where it lives | Revocable |
| --- | --- | --- | --- | --- |
| Access | JWT (HS256) | **15 minutes** | Client memory | No — it expires |
| Refresh | `<session-uuid>.<secret>` | **30 days** | Client storage + a hash in `sessions` | Yes — `revoked_at` |

- The access token is verified for signature, algorithm (**HS256 only**), expiry, and type.
  A refresh token presented as an access token is rejected.
- The refresh token is **rotated on every refresh**, in one conditional `UPDATE`
  (KNOT-ADR-085). The old token stops working immediately.
- **Reuse detection:** presenting a token that was already rotated revokes **every session
  on the account** and returns `401`. A token in two hands means one holder is not the
  owner, and the server cannot tell which — so both sign in again.
- A token for a session that is already revoked (signed out) is answered `401` **without**
  escalation: there is nothing left to contain, and escalating would let a stale client
  sign the user out everywhere.
- Access tokens carry `sid`, so logout and the device list act on the caller's own session
  and never on an id taken from a request body.

## 5. Sessions

- **Maximum 5 live sessions per account** (KNOT-ADR-084). A sixth sign-in revokes the
  oldest. A failure to enforce is logged, never raised: a correct password must not fail
  because housekeeping did.
- The device list (`GET /auth/sessions`) shows the device info the client sent, the IP
  address, and when the session was created and last used. `current: true` marks the session
  the request came from.
- Revoking another account's session returns `404 session_not_found`: the API never confirms
  that an id exists on someone else's account.
- Signing out is idempotent.

## 6. Email verification and password reset

Both flows are gated on a token that is stored as a **hash**, is **single use**, and has an
expiry. Neither token is ever returned by an API response.

| Flow | Storage | Lifetime | Effect |
| --- | --- | --- | --- |
| Email verification | `verification_tokens` | **24 hours** | Sets `users.email_verified_at` |
| Password reset | Redis (`knot:auth:reset:<hash>`) | **30 minutes** | Sets a new password and revokes **every** session |

- `POST /auth/forgot-password` and `POST /auth/resend-verification` **always** answer
  `200 { "success": true }`, for registered, unregistered, already-verified, and
  malformed addresses alike (KNOT-ADR-087, KNOT-ADR-090).
- Reset order is: write the password → revoke every session → delete the token. If
  revocation fails the token is still valid, so the client can retry, and the account never
  ends up with a new password and sessions authorised by the old one.
- **The links are emailed through Resend** (KNOT-ADR-090). The server console is **no longer**
  a credential store: the only time a link reaches a log is local development with
  `KNOT_RESEND_API_KEY` unset, where the no-op sender logs the message it would have sent.
  A deployment that sets the key never writes a link to stdout.
- Sending is **best-effort**: a provider failure is logged and the flow still completes, so a
  Resend outage cannot stop somebody registering or asking for a reset. The person can ask
  for another link with `POST /auth/resend-verification`.
- The API key itself is read from `KNOT_RESEND_API_KEY` and is never logged or returned
  (`Config` has no `String` method).

## 7. Rate limits

All counting happens in Redis (`knot:auth:rl:<limit>:<key>`), so limits apply across every
instance of the API rather than per process. A refused request gets
`429 rate_limited` with a `Retry-After` header whose value is the **whole window** — the
longest the caller can possibly need to wait.

| Endpoint | Key | Limit |
| --- | --- | --- |
| `POST /auth/register` | client address | 5 per hour |
| `POST /auth/login` | client address | 10 per 15 minutes |
| `POST /auth/login` | normalised email | 10 per 15 minutes |
| `POST /auth/forgot-password` | normalised email | 3 per hour |
| `POST /auth/resend-verification` | normalised email | 3 per hour |
| `POST /users/me/recover` | client address | 10 per 15 minutes (shared with the login counters) |
| `POST /users/me/recover` | normalised email | 10 per 15 minutes (shared with the login counters) |
| `POST /auth/refresh` | session id | 30 per 15 minutes |
| `POST /auth/forgot-password` | normalised email | 3 per hour |

The per-account sign-in limit matches the per-address one at **10 per 15 minutes** so
that the five-device session cap (KNOT-ADR-084) is actually reachable: at the previous
five, the limiter refused the sixth sign-in before the cap could demonstrate itself.

`verify-email` and `reset-password` are **not** rate limited: their tokens are already
single use, unguessable, and expiring, and a limiter keyed on an address would let an
attacker lock a legitimate user out of redeeming their own link.

**Two deliberate weaknesses, stated plainly:**

1. **The limiter fails open, by design.** If Redis is unavailable the check logs a warning
   and allows the request. Failing closed would turn a cache outage into a total sign-in
   outage for every user, so availability was chosen over enforcement. **This is an
   accepted MVP decision**, not an oversight: the consequence is that whoever can take
   Redis down also removes the limits, so Redis must be protected like any other security
   dependency, and the decision should be revisited before Knot handles real personal
   data at scale.
2. **The address is the connection's, not a header's.** `clientIP` reads `RemoteAddr` and
   ignores `X-Forwarded-For`, because the API is not behind a proxy today and trusting that
   header would let any caller choose its own limiter key. When a proxy is introduced,
   `clientIP` is the single place that must learn which header is trustworthy — and until it
   does, every request behind the proxy shares one key.

## 8. What an attacker learns

| Attempt | Response |
| --- | --- |
| Unknown address, sign-in | `401 invalid_credentials` — identical to a wrong password |
| Wrong password | `401 invalid_credentials` |
| Correct password, unverified address | `403 email_not_verified` (they already knew the password) |
| Deleted account inside its window, correct password | `200` — the account is recovered, and the response says `recovered: true` |
| Deleted account past its window, not yet purged | `410 account_pending_purge`, with or without the right password (the password is still checked first) |
| A purged account | `401 invalid_credentials` — its address no longer exists, so there is nothing to enumerate |
| `forgot-password`, any address | `200 { "success": true }` |
| Replayed refresh token | `401 invalid_refresh_token` — the same answer as a garbage token |
| Another account's session id | `404 session_not_found` |

## 9. Account deletion

Deletion is reversible for 30 days, then anonymises the account (KNOT-ADR-081,
KNOT-ADR-089). The security-relevant consequences are these.

**Deleting ends every session, before the account is marked.** `RequestDeletion` calls
`SessionRevoker.RevokeAllForUser` and only then sets `deleted_at`. If revocation fails the
deletion is refused, so the caller is never told an account is gone while a token for it is
still honoured. Refresh tokens die with the row: `FindActiveUserByID` filters deleted
accounts, so no device can renew a session after the deletion.

**An access token already in flight survives up to fifteen minutes, and this is stated
rather than papered over.** Access tokens are stateless (KNOT-ADR-080) and the middleware
verifies signature, algorithm, expiry and type only — it does not read the session row —
so revoking a session stops the *next* token, not the current one. A deleted account's
access token therefore still opens protected routes until it expires, which is the same
window every other revocation in the system has (logout, session revoke, password reset).
Clients are what make sign-out feel immediate: the app drops its token on deletion. Closing
the server-side gap means checking user or session state on every protected request, which
is a decision about the token architecture as a whole, not one to take in the deletion
path. Until then, "signed out of all devices immediately" means every session is revoked
immediately — no device can obtain another token — not that a token already minted stops
verifying.

**The deletion request requires the password even though the caller is authenticated.**
An unlocked phone in a settings list should not be able to destroy an account in two taps,
and the password is the only second factor Knot has.

**`POST /users/me/recover` is public, and that is deliberate.** Everyone who needs it has
just been signed out of every device, so requiring a token would ask for the credential the
deletion destroyed. Email plus password is the credential, and its failure answer is the
same `401 invalid_credentials` that sign-in uses, so the route cannot be used to find out
which addresses exist. Signing in is an equivalent path, so there is no way to "forget" the
recovery endpoint and lose an account.

**Purging is irreversible and is guarded by one shared secret.** `POST /admin/purge-deleted`
is guarded by `X-Knot-Admin-Secret`, compared with `subtle.ConstantTimeCompare`, and never
logged — not even on failure, because the failure path of a shared secret is where one
leaks. `KNOT_ADMIN_SECRET` has a local default and is **required outside `local`**; a
handler constructed without one refuses to exist rather than accepting every request. In
effect the secret is as sensitive as the database credential: whoever holds it can purge
every due account. There is no user-facing route to it and no scheduler yet (KNOT-034a).

**What survives the purge, and why.** The account row, `deleted_at`,
`deletion_scheduled_at`, and the content the person wrote. Attribution becomes the literal
`Deleted User`. A hard delete would cascade through stories, comments, reactions and
adaptations that other people are reading, so "delete my account" is implemented as "stop
treating this as me", which is the honest description of what the platform can promise.

**What does not survive.** `email` (replaced with `purged+<id>@deleted.invalid`),
`display_name` (`Deleted User`), `phone`, `approximate_location`, `avatar_url`,
`preferred_languages`, `password_hash` (`!purged`, which no argon2id comparison can match),
and every row in `credentials`, `sessions` and `verification_tokens`. A purged account
cannot authenticate by any route: its address no longer exists, so sign-in answers the
ordinary `401 invalid_credentials`.

**Deletion does not touch Redis rate-limit counters.** They expire on their own within
their windows, and clearing them would be a way for an attacker to reset a limit by
deleting an account.

## 10. What is deliberately not here yet

- **No delivery guarantees for email.** Sending is best-effort (KNOT-ADR-090): a Resend
  outage, a bad address, or an unverified sending domain produces a log line, not a retry
  queue. There is no bounce handling and no fallback provider.
- **No OAuth or magic links** beyond the closed `credentials` type set.
- **No scheduled purge.** `POST /admin/purge-deleted` exists and anonymises correctly, but
  nothing calls it on a timer; until KNOT-034a lands, an operator has to. A deployment
  that never calls it leaves every deleted account inside its window forever.
- **No GDPR-style hard delete.** Deletion anonymises the account row and keeps the content,
  which is a product decision (KNOT-ADR-081), not an erasure implementation.
- **No 2FA, no device fingerprinting, no impossible-travel detection.**
- **AsyncStorage is not secure storage.** On mobile the refresh token is stored in plain
  AsyncStorage (KNOT-ADR-024), and a follow-up task moves it to Keychain/Keystore.
- **No per-account global lockout.** A distributed attack spread across many addresses is
  slowed by the per-account login limit, not stopped by it.

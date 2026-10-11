# Knot — Safety Model

This is the summary of how Knot keeps people safe. It covers what ships today
(reports and blocks, KNOT-017a) and what is deliberately deferred (the moderator
queue and actions, KNOT-017b). Human moderation is foundational (Master Brief §14);
there is **no automated moderation** and no algorithm that hides content.

## The shape of the system

| Piece | Status | What it does |
| --- | --- | --- |
| Roles | shipped | `users.role` (`user` / `moderator` / `admin`), assigned by hand |
| Blocks | shipped | one user hides another, both ways, and stops interaction |
| Reports | shipped | a user flags an entity for a moderator to review |
| Moderation cases | shipped (collection) | reports on one entity aggregate into one case |
| Audit log | shipped | an append-only trail of block and report events |
| Moderator queue | **KNOT-017b** | the list of open cases a moderator reviews |
| Moderator actions | **KNOT-017b** | hide / warn / suspend / dismiss / unhide |

## Reports

Any user-generated content is reportable: stories, versions, comments, bridges,
inquiries, and answers. A report names one of six categories — `harassment`,
`hate_speech`, `misinformation`, `spam`, `sensitive_content`, `other` — and, for
`other`, a free-text reason (KNOT-ADR-062).

- A user may report a given entity **once**; a repeat is a 409, not a second row
  (KNOT-ADR-061).
- Reports on the same entity from different users aggregate into **one**
  moderation case with a running `report_count`, so a moderator sees one queue row
  per entity rather than one per report.
- Reports are **private**: neither the reporter nor the reported user is shown to
  the other, and there is no public record of a report.
- Reports are **not** auto-acting. Filing one changes nothing about who can see
  the content; it only adds to a moderator's queue.

## Blocks

Blocking is **mutual hiding plus write prevention** (KNOT-ADR-060).

- **Hiding.** If A blocks B, A stops seeing B's content and B stops seeing A's —
  in the feed, comment threads and replies, bridges, inquiries and answers, and
  profile walls. A blocked profile answers 404.
- **Write prevention.** Neither side can comment on, bridge, react to, answer, or
  adapt the other's content; such a write is refused with 403 `blocked`. The
  client shows "You cannot interact with this content."
- **Silence.** Blocking notifies no one: the blocked user is not told.
- **Reversible.** Unblocking restores content and interaction immediately, because
  the filter is applied at read time rather than by marking rows hidden.

A blocked relationship is resolved once per authenticated request, so the cost to
a page is one predicate, not a query per author.

## Roles and the queue (KNOT-017b)

Roles are assigned manually at MVP: there is no API to grant one, which keeps the
privilege-escalation surface at zero (KNOT-ADR-059). The moderator queue and its
actions ship in KNOT-017b; when they do, every action (hide, warn, suspend,
dismiss, unhide) will write to the audit log so a moderation decision is always
accountable.

## What is out of scope

No automated moderation, no appeals workflow, no account deletion, and no
analytics dashboard. Content reports and blocks are silent: neither fires a
notification in KNOT-017a.

# Knot — UX Reference

> **This is reference intent, not approved scope.** The screens below are transcribed from
> the original ten-screen mobile concept. Some have shipped and are named here; most have
> not. A feature described in this document becomes real only when its task is dispatched —
> see [`docs/ROADMAP.md`](ROADMAP.md) and
> [`docs/ENGINEERING_WORKFLOW.md`](ENGINEERING_WORKFLOW.md). Where the concept and the
> shipped product differ, the shipped product wins, and the difference is noted below.

Phase labels used on each entry:

- **Phase 1** — identity, stories, Tell My People, bridges (the core loop).
- **Phase 2** — Rooted, Discovery Map, Curious Inquiries, notifications.
- **Post-MVP** — everything after those.

Shipped screens are cross-referenced by their component name, and the nine that exist today
are inventoried in [`docs/SCREENS.md`](SCREENS.md).

---

## 1. Splash

**Concept.** The brand's first frame: the Knot logomark or primary logo on the brand surface
while the app loads, with the primary tagline underneath. It sets tone before it asks for
anything.

**Current status.** **Not Yet Built.** No branded launch screen exists; the app opens
directly on the auth screens. The assets and colours it needs are specified in
[`docs/BRAND.md`](BRAND.md).

**MVP phase.** Phase 1 (app shell and branding).

## 2. Onboarding

**Concept.** Two or three first-run panels that explain what Knot is — stories told in many
languages, adapted by people rather than machines, joined by bridges — ending at Register or
Login.

**Current status.** **Not Yet Built.** The app opens on `RegisterScreen`, with a link that
swaps it for `LoginScreen`.

**MVP phase.** Phase 1.

## 3. Login

**Concept.** Email and password sign-in, with a route to account creation beside it.

**Current status.** **Shipped.** `LoginScreen`, paired with `RegisterScreen`; the two swap
via a link rather than through navigation. A session is issued on success and held in memory.

**MVP phase.** Phase 1.

## 4. Home Feed

**Concept.** The public feed of stories, newest first. Each card shows the title, pillar,
language, coarse place, and whether the story is sensitive, and there is an obvious way to
publish one.

**Current status.** **Shipped.** `FeedScreen`. Keyset paging with a "Load more" control,
pull-to-refresh on the list, the signed-in email, a sign-out action, and a "Tell a story"
entry point.

**MVP phase.** Phase 1.

## 5. Post Detail with Language Tree

**Concept.** One story in full, with the language tree shown _alongside or beneath it_ so a
reader can see the other tellings without leaving the post, plus the conversation on the
telling they are reading.

**Current status.** **Partially Shipped.** `StoryDetailScreen` shows the story in full from
its root version and offers three entry points — "Adapt for my people", "View language tree",
and "See conversation". The tree is a **separate screen**, not an inline preview inside the
post, so the concept's side-by-side arrangement is not built.

**MVP phase.** Phase 1.

## 6. Language Tree

**Concept.** The visual tree of a story's versions: the original at the root, adaptations
branching from it, each labelled with its language, so a reader can see how one telling grew
out of another.

**Current status.** **Shipped as an indented list.** `LanguageTreeScreen` renders every
version of a story indented by its depth, with a language badge, an author prefix, the
adaptation note, and a "root" marker. A graphical tree layout is not built, and a per-version
bridge-count badge is deferred.

**MVP phase.** Phase 1.

## 7. Comments

**Concept.** The conversation under a telling, with an easy way to reply across a language
boundary.

**Current status.** **Shipped.** `CommentThreadScreen` — a flat, newest-first, cursor-paged
list with a compose box, pull-to-refresh, and a "Bridge to another language" action on every
comment — and `BridgeScreen`, which writes a new comment in a target language and joins it to
the one it came from. Replies happen **only** by bridging; comment threading is deferred.

**MVP phase.** Phase 1.

## 8. Discovery Map

**Concept.** A map of places and languages, letting someone browse stories by where they come
from and what is spoken there.

**Current status.** **Not Yet Built.** Confirmed for Phase 2 in the roadmap, because it needs
stories and their language versions to exist before it can say anything useful.

**MVP phase.** Phase 2.

## 9. Create Story

**Concept.** Compose a story: a pillar, a language, a title, the story itself, a coarse place,
media, and a sensitive flag.

**Current status.** **Shipped, text-first.** `CreateStoryScreen` publishes pillar, language,
title, body, approximate location, **one** media link, and the sensitive flag. Multiple or
richer media is not built — media storage strategy is Phase 2 work.

**MVP phase.** Phase 1 (text); media handling in Phase 2.

## 10. Cultural Profile

**Concept.** A person's cultural identity as a page others can visit: the languages they
speak, the places they are from, and the heritage they carry.

**Current status.** **Not Yet Built.** Accounts exist — `RegisterScreen` and `LoginScreen`
already collect preferred languages and an approximate location — but there is no profile
screen, and the concept's profile is richer than the current model stores.

**MVP phase.** Phase 1 (the roadmap lists profiles under Phase 1).

---

## Where the concept and the product diverged

Two differences are worth keeping in view, because they explain why some concept screens look
further away than others:

- **The language tree is a place, not a preview.** The concept put the tree inside the post;
  the product made it its own screen reached from the post. That is a deliberate simplicity
  choice, not a missing feature — see the Post Detail and Language Tree entries above.
- **"Messages" is not an inbox.** The concept's bottom navigation has a Messages destination.
  Knot's actual conversations are per-version comment threads, reached from a story, rather
  than a global message list. The navigation implications are covered in
  [`docs/NAVIGATION.md`](NAVIGATION.md).

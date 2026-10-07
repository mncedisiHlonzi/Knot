# Knot — Navigation

> **Status: proposed, not yet approved.** This document describes the navigation model Knot
> intends to adopt. It is not a specification of what the app does today, and it is not
> approved scope. The final design will be confirmed in a future task; until then, treat
> everything below the "Current state" section as a proposal.

## Primary navigation

The concept's mobile navigation is a **bottom tab bar with five destinations**, with **Create**
raised in the centre:

```
   Home        Map       ( + )      Messages      Profile
```

- **Home** — the story feed.
- **Map** — explore stories and languages by place (Discovery Map).
- **Create** (centre, raised) — publish a story; the primary action, reachable from anywhere.
- **Messages** — conversations.
- **Profile** — the signed-in person's own cultural profile.

Two of the five destinations have no screen behind them yet (Map and Profile), which is the
main reason the tab bar is not built: a tab that leads nowhere is worse than no tab.

## Current state

The app **has no navigation library**. `apps/mobile/App.tsx` holds a single `ScreenName` string
in component state —

```ts
type ScreenName =
  | 'register' | 'login' | 'feed' | 'detail' | 'create' | 'adapt' | 'tree' | 'comments' | 'bridge';
```

— and renders exactly one screen for the current value. Transitions are explicit, typed
callbacks (`onBack`, `onOpenStory`, `onAdapt`, …) passed down to each screen. There are **no
routes, no deep links, and no back gestures**; the hardware/gesture back button is not wired to
anything. The session and the access token live in component state only and are lost when the
app restarts.

That is deliberate for a shallow app. Every transition is one `setScreen(...)` call, the type
checker proves the destinations, and there is no router dependency to justify.

## Auth flow

**Today (implemented):** the app renders the auth screens whenever there is no session —
`RegisterScreen` by default, with a link that swaps it for `LoginScreen` — and renders the
story screens only once a session exists. In other words, **the feed currently requires
signing in**. Sign-out returns to Register.

**Proposed:** unauthenticated people browse **read-only** — Feed and StoryDetail, and the
conversations and trees behind them — and see **Create** and the other write actions only once
they are authenticated. The API already allows this: the reads
(`GET /stories`, `GET /stories/{id}`, `GET /stories/{id}/tree`, `GET /versions/{id}`,
`GET /versions/{id}/comments`, `GET /comments/{id}/bridges`, `GET /bridges/{id}`) are public,
and only the writes require a bearer token. So the change is client-side, and it needs an
approved task.

## Stack flows

The flows below are the proposal. Today each hop is a `setScreen` call rather than a pushed
route, and the "back" targets are hard-coded rather than derived from a stack:

```
Feed
 └── StoryDetail
      ├── AdaptStory ──▶ (on success) LanguageTree
      ├── LanguageTree
      └── CommentThread
           └── Bridge ──▶ (on success / back) CommentThread
```

- **Feed → StoryDetail** — tapping a story opens it.
- **StoryDetail → AdaptStory** — "Adapt for my people", starting from the story's **root
  version**.
- **StoryDetail → LanguageTree** — "View language tree".
- **StoryDetail → CommentThread** — "See conversation", on the story's **root version**.
- **AdaptStory → LanguageTree** — a new adaptation is a new version, so the tree is where the
  person lands.
- **CommentThread → Bridge** — "Bridge to another language" on a comment.
- **Bridge → CommentThread** — a successful bridge (or cancelling) returns to the thread, which
  reloads and shows the newly bridged comment.

## Modals and temporary screens

**Create Story** is a **modal-style flow**: it is entered from the feed, it is about one task,
and it should slide up over the feed and dismiss back to it, rather than being a stack
destination. Today it is a plain screen; today's behaviour after publishing is to open the new
story's detail, which the modal model would revisit.

## Migration plan

**When to introduce a navigation library.** When the bottom tab bar actually exists — that is,
**after the Discovery Map lands (post-KNOT-008)**, when there are genuinely several top-level
destinations and back behaviour, tab state, and deep links all start to matter. That is the
point at which a router stops being overhead and starts being the cheapest way to express the
app's structure.

**Why not yet.** A router (for example `react-navigation`) would add a dependency and a
navigation model to serve **one** shallow stack where every transition is already explicit and
type-checked. It would buy back-gesture and deep-link behaviour the product does not ask for
yet, at the cost of the simplicity the current state machine has; see **KNOT-ADR-002** (mobile
scaffold hand-authored) and **KNOT-ADR-007** (minimal dependency set) for the same discipline
applied elsewhere. Adding it before the destinations exist would mean designing navigation for
screens that have not been built.

**Recommendation.** Dispatch a task to introduce navigation (a tab navigator plus a stack per
tab, most likely `react-navigation`) at the moment the tab bar is built — post-KNOT-008, once
Discovery Map has landed — and confirm this document's proposal at that point rather than
treating it as settled now.

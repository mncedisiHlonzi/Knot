# Knot — Navigation

> **Status: partly implemented (KNOT-008).** The bottom tab bar described here now exists, with
> **four** destinations — Home, Map, Create, and Profile — implemented as a hand-rolled tab bar
> plus a single overlay slot, not a navigation library (KNOT-ADR-019). The **Messages**
> destination, per-tab stacks, deep links, and back gestures remain **proposals**; treat those
> parts of this document as not yet built.

## Primary navigation

The app's primary navigation is a **bottom tab bar**, implemented in KNOT-008:

```
   Home          Map          Create          Profile
```

- **Home** — the story feed.
- **Map** — explore stories by place (the Discovery Map).
- **Create** — publish a story.
- **Profile** — the signed-in person's own cultural profile.

The original concept proposed **five** destinations, with **Create** raised in the centre and a
**Messages** tab. The implementation ships **four**: Create is an ordinary tab rather than a
raised centre action, and Messages is not built — conversations are still reached from a story's
detail screen. A tab that leads nowhere is worse than no tab, so Messages waits until there is a
conversation inbox to put behind it.

## Current state

The app **has no navigation library**. `apps/mobile/App.tsx` models the app in two levels:

```ts
type TabName = 'feed' | 'discoveryMap' | 'createStory' | 'profile';

type Overlay =
  | { name: 'detail'; storyId: string }
  | { name: 'adapt'; storyId: string; parentVersionId: string }
  | { name: 'tree'; storyId: string }
  | { name: 'comments'; storyId: string; versionId: string }
  | { name: 'bridge'; storyId: string; versionId: string; comment: Comment }
  | { name: 'rootedSetup' }
  | { name: 'placeStories'; place: string };
```

An **active tab** is one of the four primary destinations, and an optional **overlay** is a
secondary screen pushed over it. `TabBar` — a plain row of four buttons — is rendered **only
when there is no overlay**, so a secondary screen replaces the whole surface rather than sitting
inside the bar. Transitions are explicit, typed callbacks (`onBack`, `onOpenStory`,
`onOpenPlace`, …), and each overlay carries the ids it needs to return to where it came from.
There are still **no routes, no deep links, and no back gestures**; the hardware/gesture back
button is not wired to anything. The session and the access token live in component state only
and are lost when the app restarts.

That remains deliberate. Every transition is one `setState` call, the type checker proves the
destinations, and there is no router dependency to justify; see KNOT-ADR-019.

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

The flows below are implemented, as `setOverlay(...)` calls with hard-coded back targets (there
is still no stack to derive them from):

```
Home (feed) ──────────▶ StoryDetail
Map (discoveryMap) ───▶ PlaceStories ───▶ StoryDetail

StoryDetail
 ├── AdaptStory ──▶ (on success) LanguageTree
 ├── LanguageTree
 └── CommentThread
      └── Bridge ──▶ (on success / back) CommentThread
```

- **Feed → StoryDetail** — tapping a story opens it.
- **Map → PlaceStories** — tapping a place marker or a list row opens that place's stories.
- **PlaceStories → StoryDetail** — tapping a story opens it.
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

**Create Story** is now a **tab**, not a modal: KNOT-008 made Create one of the four primary
destinations, so it is entered from the bar and returns to Home when cancelled. After
publishing, the app still opens the new story's detail as an overlay. If Create later needs to
feel like a modal — slide up, dismiss back to where you were — that is a change to revisit; it is
not the implemented model today.

## Migration plan

**When to introduce a navigation library.** When the app's navigation genuinely outgrows a tab
bar plus one overlay — when deep links, back gestures, or a per-tab stack start to matter.
KNOT-008 landed the tab bar **without** a router (see KNOT-ADR-019), so the trigger has not been
reached.

**Why still not.** A router (for example `react-navigation`) would add a dependency and a
navigation model to serve four tabs and a single overlay slot, where every transition is already
explicit and type-checked. It would buy back-gesture and deep-link behaviour the product does not
ask for yet, at the cost of the simplicity the current state machine has; see **KNOT-ADR-002**
(mobile scaffold hand-authored) and **KNOT-ADR-007** (minimal dependency set) for the same
discipline applied elsewhere.

**Recommendation.** Keep the hand-rolled tab bar until a concrete flow needs what it cannot
express — a deep link into a nested screen, a real back stack within a tab, or a modal that must
survive a tab switch. At that point, adopt a router and convert this document's remaining
proposals into that decision.

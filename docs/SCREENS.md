# Knot — Screens

The mobile screen inventory: what exists today, what is planned, and how the concept's
navigation maps onto it. "Shipped" means the screen is implemented in
`apps/mobile/src/screens/` and reachable from the app's state machine. Everything else is
intent, and an entry becomes real only when its task is dispatched (see
[`docs/ENGINEERING_WORKFLOW.md`](ENGINEERING_WORKFLOW.md)).

Phase labels: **Phase 1** — identity, stories, Tell My People, bridges. **Phase 2** — Rooted,
Discovery Map, Curious Inquiries, notifications. **Post-MVP** — everything after those.

## Shipped screens

| Screen | Category | Phase | Status | Task | Description |
| --- | --- | --- | --- | --- | --- |
| Register | Auth | Phase 1 | Shipped | KNOT-003 | Create an account and receive a session. |
| Login | Auth | Phase 1 | Shipped | KNOT-003 | Sign in and receive a session. |
| Feed | Stories | Phase 1 | Shipped | KNOT-004 | Browse the public story feed, newest first, with keyset paging. |
| StoryDetail | Stories | Phase 1 | Shipped | KNOT-004 | Read one story in full, with entry points to adapt it, view its tree, and open its conversation. Extended by KNOT-005 and KNOT-006. |
| CreateStory | Stories | Phase 1 | Shipped | KNOT-004 | Publish a story: pillar, language, title, body, coarse place, one media link, and a sensitive flag. |
| AdaptStory | Tell My People | Phase 1 | Shipped | KNOT-005 | Retell an existing version in another language, prefilled from the version being adapted. |
| LanguageTree | Tell My People | Phase 1 | Shipped | KNOT-005 | See every version of a story as an indented list, by depth, with language badges. |
| CommentThread | Conversations | Phase 1 | Shipped | KNOT-006 | Read a version's comments newest-first and post a new one. Replies expand inline, collapsed by default, and a composer language chip — added by KNOT-015e and reworked by KNOT-015e-fix. |
| Bridge | Conversations | Phase 1 | Shipped | KNOT-006 | Bridge a comment into another language, joining two conversations. |

Nine screens shipped; the same list is visible in the `ScreenName` state machine in
`apps/mobile/App.tsx`, which is documented in [`docs/NAVIGATION.md`](NAVIGATION.md).

## Concept and planned screens

| Screen | Category | Phase | Status | Task | Description |
| --- | --- | --- | --- | --- | --- |
| Splash | Onboarding | Phase 1 | Concept | — | Branded launch screen: logomark and primary tagline while the app loads. |
| Onboarding | Onboarding | Phase 1 | Concept | — | First-run panels explaining the loop, ending at Register or Login. |
| Cultural Profile | Profile | Phase 1 | Planned | — | A person's languages, places, and heritage, as a page others can visit. |
| Discovery Map | Discovery | Phase 2 | Planned | — | Browse stories and languages by place. Confirmed for Phase 2 in the roadmap. |
| Rooted | Community | Phase 2 | Concept | — | Trust and standing signals for contributors. |
| Curious Inquiries | Discovery | Phase 2 | Concept | — | Question-led discovery over stories and languages. |
| Notifications | Community | Phase 2 | Concept | — | Alerts when a story is adapted, commented on, or bridged. A surface rather than a single screen. |

None of the entries in this second table has a dispatched task yet, so none carries a
`KNOT-XXX` reference. `docs/UX_REFERENCE.md` describes each concept screen in full and states
what has shipped in its place.

## Bottom navigation (concept)

The concept's mobile navigation is a five-destination bottom tab bar with **Create** raised in
the centre. It is not implemented; the app currently uses a screen-name state machine. How the
destinations map onto what exists today:

| Destination | Concept purpose | Maps to today |
| --- | --- | --- |
| **Home** | The feed of stories. | `FeedScreen` (shipped). |
| **Map** | Explore stories and languages by place. | Discovery Map (Phase 2; not built). |
| **Create** (centre) | Publish a story. | `CreateStoryScreen` (shipped, reached from the feed rather than a tab). |
| **Messages** | Conversations. | `CommentThreadScreen` and `BridgeScreen` (shipped, but per-version threads reached from a story — not a global inbox). |
| **Profile** | A person's cultural identity. | The activity wall (`UserProfileScreen`) on the Profile tab, showing who a person is and everything they have authored (KNOT-015c, KNOT-015d). The wider Cultural Profile — languages and places as a page of their own — is still planned. |

Four of the five destinations have a shipped screen behind them; one (Map) does not
yet exist. The tab bar itself is deliberately deferred until the destinations do — the
reasoning is in [`docs/NAVIGATION.md`](NAVIGATION.md).

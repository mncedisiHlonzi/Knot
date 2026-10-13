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
| Splash | Onboarding | Phase 1 | Shipped | KNOT-020c | The cold-start screen: the Knot logo and tagline on a white canvas while the stored session and the first-run flag are read. Auto-advances; no controls. |
| Onboarding | Onboarding | Phase 1 | Shipped | KNOT-020c | Three first-run slides (Skip, and Continue → Get started on the last) explaining the loop, ending at the auth screens. Shown once; the flag is persisted in AsyncStorage. |
| Register | Auth | Phase 1 | Shipped | KNOT-003 | Create an account and receive a session. |
| Login | Auth | Phase 1 | Shipped | KNOT-003 | Sign in and receive a session. |
| Feed | Stories | Phase 1 | Shipped | KNOT-004 | Browse the public story feed, newest first, as a continuous hairline-separated column with pull-to-refresh and keyset paging. Each entry carries the author block (name + quiet Rooted line), a plain pillar tag, full-bleed media, and a text meta line; a video auto-plays muted while its entry is on screen, behind a feed-wide mute toggle. Redesigned by KNOT-020c and polished by KNOT-020c-fix, -fix2, and -fix3. |
| StoryDetail | Stories | Phase 1 | Shipped | KNOT-004 | Read one story in full as an edge-to-edge reading flow (hero, pillar tag, author line, language row, body, signals), with entry points to adapt it, view its tree, and open its conversation. Redesigned by KNOT-020c. Extended by KNOT-005 and KNOT-006. |
| CreateStory | Stories | Phase 1 | Shipped | KNOT-004 | Publish a story: pillar, language, title, body, coarse place, one media link, and a sensitive flag. |
| AdaptStory | Tell My People | Phase 1 | Shipped | KNOT-005 | Retell an existing version in another language: a quiet source preview, then a tree-marked form (language picker, title, the retelling, an optional note) that prefills from the version being adapted. Redesigned by KNOT-020d. |
| LanguageTree | Tell My People | Phase 1 | Shipped | KNOT-005 | See every version of a story as a tree: a continuous indigo spine with curved elbows, a filled marker for the root and hollow ones for its children, each row naming the language, its code, and its author, with a footer action to tell the story in your own language. Redesigned by KNOT-020d; each row now opens the version's detail and no longer carries an inline reaction bar or Rooted badge (KNOT-020d-fix). |
| StoryVersion | Tell My People | Phase 1 | Shipped | KNOT-020d | Read one version of a story on its own: the language and its ISO code, the author's attribution (with the Rooted badge), the title, the body, the adapter's note when one was left, the four perspective signals, and actions to adapt this version or read the full story. Opened by tapping a Language Tree row (KNOT-020d-fix). |
| CommentThread | Conversations | Phase 1 | Shipped | KNOT-006 | Read a version's comments newest-first as a flat, hairline-separated thread and post a new one. Replies expand inline — collapsed, then the first few, then all — hanging off a hairline spine, and the composer pairs a language chip with the input. The expand behaviour came from KNOT-015e and its rework; the design pass is KNOT-020d; comments no longer carry an inline Rooted badge (KNOT-020d-fix). |
| Bridge | Conversations | Phase 1 | Shipped | KNOT-006 | Bridge a comment into another language, joining two conversations: the source comment framed by a hairline, then a target-language picker and the adapted comment with an optional note. Redesigned by KNOT-020d. |
| Profile (own) | Profile | Phase 1 | Shipped | KNOT-020c | The signed-in user's wall: a centred header (avatar, name, Rooted badge, joined date, stories/versions/answers stats) above six activity tabs and a compact activity list. A redesign of the wall shipped in KNOT-015c / KNOT-015d. |

Thirteen screens shipped; the same list is visible in the `ScreenName` state machine in
`apps/mobile/App.tsx`, which is documented in [`docs/NAVIGATION.md`](NAVIGATION.md).

## Concept and planned screens

| Screen | Category | Phase | Status | Task | Description |
| --- | --- | --- | --- | --- | --- |
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

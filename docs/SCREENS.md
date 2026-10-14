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
| Discovery Map | Discovery | Phase 2 | Shipped | KNOT-020e | Browse the places stories are told in: a Mapbox map (light style, KNOT-ADR-026) across the screen behind a rounded bottom sheet, a size-scaled marker per cluster (keyed by place and country), a pillar filter (All / Wonder / Heritage) as underline tabs, and the scrollable place list with pull-to-refresh inside the sheet. Tapping a marker or a row opens that place's stories. Redesigned by KNOT-020e; reworked by KNOT-020e-fix. |
| Place Stories | Discovery | Phase 2 | Shipped | KNOT-020e | Every story told at one place, newest first: the place as the screen's headline with its story and pillar totals beneath it, a compact **non-interactive map strip** pinning where the place is (drawn only when the place's cluster carried a coordinate), a **pillar filter** (All / Heritage / Wonder) that refetches the list server-side, then a list of `FeedEntry` rows (the same component the Feed uses) with pull-to-refresh and cursor paging on scroll-to-end. Opened from the Discovery Map. Redesigned by KNOT-020e; the map header is KNOT-020e-fix3; the pillar tabs are KNOT-020e-fix6. |
| Search | Discovery | Phase 1 | Shipped | KNOT-018 | Full-text search across stories, versions, and inquiries: a phrase field with a 300 ms debounce, content-type / pillar / language / place filters, session-only recent phrases as chips, and flat hairline result rows — an overline kind label, the title, a two-line preview, the author line, and the pillar tag. Results page by keyset; tapping one opens the story, its version tree, or the inquiry. Redesigned by KNOT-020f. |
| Notifications | Community | Phase 2 | Shipped | KNOT-015 | The in-app notification inbox (KNOT-ADR-039): who adapted your version, commented on it, bridged one of your comments, reacted to it, or asked about your place, newest first, with pull-to-refresh and keyset paging. Each row is the actor's avatar, the sentence, and the relative time, carrying an indigo dot while unread; tapping a row marks it read and opens what it can. Delivery is in-app only — there is no push channel. Redesigned by KNOT-020f. |
| Profile (own) | Profile | Phase 1 | Shipped | KNOT-020c | The signed-in user's wall: a centred header (avatar, name, Rooted badge, joined date, stories/versions/answers stats) above six activity tabs and a compact activity list. A redesign of the wall shipped in KNOT-015c / KNOT-015d. |

Seventeen screens shipped; the same list is visible in the `ScreenName` state machine in
`apps/mobile/App.tsx`, which is documented in [`docs/NAVIGATION.md`](NAVIGATION.md).

## Concept and planned screens

| Screen | Category | Phase | Status | Task | Description |
| --- | --- | --- | --- | --- | --- |
| Cultural Profile | Profile | Phase 1 | Planned | — | A person's languages, places, and heritage, as a page others can visit. |
| Rooted | Community | Phase 2 | Concept | — | Trust and standing signals for contributors. |
| Curious Inquiries | Discovery | Phase 2 | Concept | — | Question-led discovery over stories and languages. |

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
| **Map** | Explore stories and languages by place. | `DiscoveryMapScreen` (shipped), opening `PlaceStoriesScreen` for one place. |
| **Create** (centre) | Publish a story. | `CreateStoryScreen` (shipped, reached from the feed rather than a tab). |
| **Messages** | Conversations. | `CommentThreadScreen` and `BridgeScreen` (shipped, but per-version threads reached from a story — not a global inbox). |
| **Profile** | A person's cultural identity. | The activity wall (`UserProfileScreen`) on the Profile tab, showing who a person is and everything they have authored (KNOT-015c, KNOT-015d). The wider Cultural Profile — languages and places as a page of their own — is still planned. |

All five destinations have a shipped screen behind them; the bar's own shape is documented in
[`docs/NAVIGATION.md`](NAVIGATION.md).

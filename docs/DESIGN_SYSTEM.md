# Knot — Design System

A short index of the mobile design tokens and the rules for using them. The full
specification lives in [`docs/design-reference/`](./design-reference/): open
`index.html` for a gallery of every screen in both themes, and treat
`tokens.json` as the source of truth for every value.

This page does not duplicate the spec. It explains how the tokens are wired into
the app and the handful of rules that keep the UI consistent.

Knot is **light-first** as of KNOT-020a: the app renders on a white canvas. The
dark palette is already defined in the token layer for KNOT-020i, which activates
it.

## Token architecture

Colours flow through three layers, and a screen only ever touches the third:

```
palette.ts         raw swatches        palette.light / palette.dark
    ↓
colors.ts          semantic roles      colors.bg.canvas, colors.text.primary, …
    ↓
components/        usage               background: colors.bg.canvas
```

- **`src/theme/palette.ts`** — the literal hex values from `tokens.json`, for both
  themes. This is the only file hex colours are allowed to live in.
- **`src/theme/colors.ts`** — maps a role onto a palette value. It reads
  `palette.light` today; KNOT-020i switches it to `palette.dark`, at which point
  every screen follows automatically.
- **Screens** ask for roles (`colors.text.secondary`), never raw values.

Other token files: `typography.ts`, `spacing.ts` (also exports `layout`),
`radius.ts`, `elevation.ts`, `motion.ts`. Everything is re-exported from
`src/theme/index.ts`.

A parity test (`src/theme/__tests__/tokens.test.ts`) reads `tokens.json` from
disk and fails if any token drifts from the spec.

### Legacy colour aliases (deprecated)

Before KNOT-020a the app was dark-first and `colors` exposed a different shape
(`brand.purple`, `bg.primary`, `border.subtle`, `gradient.*`, `state.error`, …).
Screens are not rewritten until their individual tasks (KNOT-020c onward), so
those keys remain as **deprecated aliases** mapped onto the nearest new palette
value. They exist only for compatibility — do not use them in new code, and
delete them as each screen migrates.

## Type scale

Two families, bundled locally as `.ttf` assets (no network fetch):

- **Inter** — the UI family (`typeScale` entries that name `fonts.ui`).
- **Plus Jakarta Sans** — hero and display headings only (`fonts.display`).

Use a **composite** style from `typeScale` (`display`, `h1`, `title`, `h2`, `h3`,
`reading`, `body`, `bodyS`, `button`, `label`, `overline`, `caption`) rather than
setting `fontSize`/`lineHeight`/`fontWeight` by hand. `overline` is upper-cased by
definition.

**Weights are one step heavier than the reference (KNOT-020c-fix6).** The app read
slightly too light on device, so the founder asked for a bolder, more modern feel
(the LinkedIn register). Every `typeScale` token's `fontWeight` is now one step
heavier than the value in `docs/design-reference/tokens.json`, **except** the three
body-level styles (`reading`, `body`, `bodyS`), which stay at 400 so paragraphs keep
their readability:

| Token | Reference | Mobile |
| --- | --- | --- |
| `display` | 800 | 800 (unchanged) |
| `h1`, `title` | 700 | **800** |
| `h2`, `h3`, `button`, `label`, `overline` | 600 | **700** |
| `caption` | 500 | **600** |
| `reading`, `body`, `bodyS` | 400 | 400 (unchanged) |

`docs/design-reference/tokens.json` is a **static design snapshot** and is
intentionally *not* edited; the divergence is declared here and enforced by
the token parity test in `src/theme/__tests__/tokens.test.ts`, which derives the
expected mobile weight from the reference spec (one step heavier, body-level
excepted). The bundled Inter and Plus Jakarta Sans sets already include the 700 and
800 weights, so no font asset is missing.

**Tabular numerals.** Any number that can change in place — counts, timestamps,
reaction tallies, progress — gets `tabularNums` so its digits stop jittering:
`style={[typeScale.caption, tabularNums]}`. Never apply it to prose.

## Spacing and layout

`spacing` is the step scale: `xs 4 · sm 8 · md 12 · lg 16 · xl 24 · xxl 32 · xxxl 48`.

`layout` names the structural insets: `screenPaddingFeed` (dense lists),
`screenPaddingReading` (reading flows), `gutter` (between cards) and
`gutterMedia` (inside a media strip). Prefer these over ad-hoc padding for screen
edges and gaps.

## Elevation

Four levels, each carrying the iOS `shadow*` props **and** the Android
`elevation`: `1` resting cards/inputs · `2` raised cards/focus · `3` floating bars
and the compose row (shadow points up) · `4` modals and sheets. Apply both props
via the one token — never set a raw shadow on a screen.

## Motion

`motion.easing` holds the standard/enter/exit curves as four-number tuples;
`motion.durations` holds `micro 110 · small 180 · medium 240 · screen 280 · max 320`
(ms). Reuse these instead of inventing timings, and never exceed `max`.

The motion pass (KNOT-020h) consumes them: **The Thread** — the 1.5pt indigo line
that draws itself (`components/TheThread.tsx`, on the splash, under the feed's
pull-to-refresh, inside the adapt form's primary button and down the language
tree while it loads), the overlay push/pop slide in `App.tsx` (`enter`/`exit` at
`durations.screen`), and the micro-interactions — a `micro` press scale on
`Button`, an animated fill plus a spring on `Chip`'s leading icon, a `micro` pulse
on a tapped `ReactionBar` pill, and a two-step `micro` bounce on the active
`TabBar` glyph. All of them run on the native driver except the chip's fill, which
animates `backgroundColor` and therefore cannot.

## Rules

- **5% indigo rule** — indigo is a scarce accent. It marks the single primary
  action or active state on a screen, not large fills; keep it to roughly 5% of
  the visible surface. Everything else uses canvas, paper, ink and the pillar
  colours.
- **Hairline-vs-card rule** — separate elements with a hairline
  (`border.hairline`) *or* with elevation, not both. Use a hairline for dividers
  and dense lists; lift a card off the canvas with elevation.
- **No-UI-gradients rule** — gradients are retired for interface chrome. All
  surfaces are flat palette fills; the legacy `colors.gradient.*` tokens are
  deprecated and must not be used.

## Fonts

The fonts are linked into the native projects from `src/assets/fonts/` via
`react-native.config.js`. Linking is a one-time, founder-side step:

```bash
cd apps/mobile
npx react-native-asset      # copies the .ttf files into ios/ and android/
# then rebuild the native app
```

Until that step runs, `fontFamily: 'Inter'` falls back to the system font; it does
not break the build.

## Reference

- `docs/design-reference/index.html` — gallery of all 22 screens, light and dark.
- `docs/design-reference/tokens.json` — the source of truth (asserted by test).
- `docs/BRAND.md` — brand voice and logo.

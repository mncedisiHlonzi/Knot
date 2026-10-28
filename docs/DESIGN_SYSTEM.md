# Knot — Design System

A short index of the mobile design tokens and the rules for using them. The
reference in [`docs/design-reference/`](./design-reference/) still describes the
**geometry** — the type scale, spacing, radius, elevation and motion curves — and
`tokens.json` remains the source of truth for those values.

**It is retired for colour.** Knot is monochrome (KNOT-020k, KNOT-ADR-096): a
neutral grey scale, pure black and pure white, and two modes designed together.
The indigo palette in the reference is history, and the parity test no longer
compares colour to it.

## Colour

Colours flow through three layers, and a screen only ever touches the third:

```
palette.ts         primitives        grey.50 … grey.950 (and the mode values)
    ↓
colors.ts          semantic roles    colors.bg.canvas, colors.text.primary, …
    ↓
components/        usage             background: colors.bg.canvas
```

- **`src/theme/palette.ts`** — the only file a literal colour may live in. It
  holds the primitive ramp and the two modes' values.
- **`src/theme/colors.ts`** — resolves a role set per mode via `getColors(mode)`,
  which caches one object per mode so stylesheet memoisation and context identity
  both hold.
- **`src/theme/ThemeContext.tsx`** — `ThemeProvider` owns the appearance and hands
  it to every screen; `useTheme()` returns `{ theme, mode, colors, setMode,
  setTheme, toggleTheme }`.
- **Screens** ask for roles (`colors.text.secondary`), never raw values, and build
  their stylesheet per mode (see *The two modes* below).

### The grey scale

Nine steps, light to dark. This is the primitive ramp:

| Step | Value | Typical role |
| --- | --- | --- |
| `grey.50` | `#FFFFFF` | light canvas, dark primary ink, brand fill in dark |
| `grey.100` | `#F5F5F5` | light sunken surface |
| `grey.200` | `#E5E5E5` | light hairline |
| `grey.300` | `#BDBDBD` | light strong rule |
| `grey.400` | `#8A8A8A` | light tertiary ink |
| `grey.500` | `#555555` | light secondary ink |
| `grey.600` | `#111111` | light body ink, dark raised surface, dark brand pressed |
| `grey.700` | `#1F1F1F` | dark hairline |
| `grey.950` | `#000000` | light primary ink, dark canvas, brand fill in light |

Nine steps cannot hold both modes' values: the contract names fourteen distinct
neutrals, and five of them fall **between** two steps. Those are declared once,
beside the ramp, in `palette.ts` (`between`), and are the only colour values a
semantic role may reach that are not a grey step:

`#0A0A0A` (dark sunken) · `#3D3D3D` (dark strong) · `#6E6E6E` (dark tertiary) ·
`#A8A8A8` (dark secondary) · `#EDEDED` (dark body) · `#333333` (light pressed) ·
`#F0F0F0` (light tint) · `#E0E0E0` (dark pressed) · `#1A1A1A` (dark tint).

State colours are the only chromatic values in the system, because danger,
success and warning must stay recognisable: `#B00020` / `#1B5E20` / `#8A5300` in
light, `#FF6B6B` / `#66BB6A` / `#FFB74D` in dark.

### The semantic contract

| Role | Light | Dark |
| --- | --- | --- |
| `bg.canvas` | `#FFFFFF` | `#000000` |
| `bg.sunken` | `#F5F5F5` | `#0A0A0A` |
| `bg.raised` | `#FFFFFF` | `#111111` |
| `bg.scrim` | `rgba(0,0,0,0.5)` | `rgba(0,0,0,0.7)` |
| `text.primary` | `#000000` | `#FFFFFF` |
| `text.body` | `#111111` | `#EDEDED` |
| `text.secondary` | `#555555` | `#A8A8A8` |
| `text.tertiary` | `#8A8A8A` | `#6E6E6E` |
| `text.inverse` | `#FFFFFF` | `#000000` |
| `border.hairline` | `#E5E5E5` | `#1F1F1F` |
| `border.strong` | `#BDBDBD` | `#3D3D3D` |
| `brand.primary` | `#000000` | `#FFFFFF` |
| `brand.pressed` | `#333333` | `#E0E0E0` |
| `brand.tint` | `#F0F0F0` | `#1A1A1A` |
| `pillar.wonder` / `pillar.heritage` | `#000000` | `#FFFFFF` |
| `state.danger` | `#B00020` | `#FF6B6B` |
| `state.success` | `#1B5E20` | `#66BB6A` |
| `state.warning` | `#8A5300` | `#FFB74D` |

Roles the app paints with that the table above does not name: `bg.separator`
(the feed's block rule, the same value as `border.hairline`), `bg.theater`
(black in both modes — the media viewer's room), `text.disabled`,
`text.onBrand` (the inverse ink, for a label on a brand fill), `text.onMedia`
(white in both modes, for text on a media scrim), `border.divider` (the same
value as `border.strong`), `scrim.overlay` (the same value as `bg.scrim`) and
`scrim.textShadow`.

### The two modes

Both modes are equal citizens, and neither is derived from the other: a role is
resolved by mode, not transformed. A screen does not read a static `colors`
object — a stylesheet created at module scope would freeze whichever mode was
loaded first — so it reads the active theme:

```ts
const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({ container: { backgroundColor: colors.bg.canvas } });

export default function Screen() {
  const { colors } = useTheme();                 // only if a colour is used inline
  const styles = useThemedStyles(createStyles); // built once per mode, cached
  …
}
```

- **The setting is System / Light / Dark.** `System` follows the device and keeps
  following it while the app runs; `Light` and `Dark` pin the mode. The choice is
  stored under `knot.appearance.v1` and applied at the root, so it survives a
  restart and takes effect on the tap. It lives on **Settings → Appearance**.
- **The switch crossfades.** A mode change repaints every surface at once, so the
  root draws the canvas being left over the app and fades it out
  (`src/motion/ThemeCrossfade.tsx`, `motion.duration.medium`).
- **The system bars follow the mode.** The status bar is declared from JS
  (`App.tsx`); the Android **navigation bar**, which RN's `StatusBar` cannot
  reach, is repainted by the app-local `SystemBarsModule` (through
  `src/native/systemBars.ts`) whenever the active mode changes, with the icon
  appearance flipped to match. `res/values/styles.xml` and
  `res/values-night/styles.xml` hold the pre-mount defaults, which follow the
  **device** appearance because a native theme cannot read the app's choice.
- **The pillar is bold text only.** `PillarTag` renders `Wonder · Manguzi` in bold,
  normal case, in `text.primary` — no overline, no underline, no letter-spacing,
  no mark, no pill, no fill. The category colours are gone with the indigo
  reference (KNOT-020k).
- **The logo is tinted where it can be.** The monochrome logo assets are single
  fill, so the splash draws pure black on the light canvas and pure white on the
  dark one. The primary logo is a gradient and the standalone logomark/wordmark
  ship only in their coloured forms, so those are unchanged until replacement
  SVGs land.

### Legacy colour aliases (deprecated)

Before KNOT-020a the app exposed a different shape (`brand.purple`, `bg.primary`,
`border.subtle`, `gradient.*`, `state.error`, …). Those keys remain as
**deprecated aliases** mapped onto the nearest monochrome role so existing imports
keep resolving. Do not use them in new code.


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

The **weight** divergence is declared here rather than in the reference, which is
why the parity test in `src/theme/__tests__/tokens.test.ts` derives the expected
mobile weight from the reference spec (one step heavier, body-level excepted). A
deliberate value change, by contrast, is made in the reference itself so the two
layers stay locked: the dark palette (KNOT-020k-fix18) and the `reading` size
(KNOT-020k-fix19) both landed in `docs/design-reference/tokens.json`. The bundled
Inter and Plus Jakarta Sans sets already include the 700 and 800 weights, so no font
asset is missing.

`reading` is **16/26** (down from 17/28, KNOT-020k-fix19): the story body takes one
step down so a long story reads tighter and more editorial at the same measure.

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

`motion.easing` holds the standard/enter/exit curves as four-number tuples — the
form `Animated` consumes — and `motion.easingNames` names the same three curves
(`easeOutCubic`, `easeOut`, `easeIn`). `motion.durations` is the original five-step
scale: `micro 110 · small 180 · medium 240 · screen 280 · max 320` (ms).
`motion.duration` is the three-step set the shared primitives pick from —
`short 150 · medium 250 · long 400` — and `motion.scale.press` is `0.97`
(KNOT-020k). Reuse these instead of inventing timings, and never exceed `max`.

**The primitives** live in `src/motion/`:

| Primitive | What it does | Used by |
| --- | --- | --- |
| `usePressScale(enabled?)` | shrinks a control to `scale.press` on press-in and back on press-out, native driver | `Button`, `Chip` |
| `useFade(initialVisible?)` | `opacity` value plus `fadeIn` / `fadeOut(done)`; `done` runs only on a completed exit, so a caller can unmount a modal after it | `MediaPickerSheet`, `ReportModal`, the profile's visitor sheet |
| `useCrossfade(isDark)` | paints the canvas being left and fades it away on a mode change | `ThemeCrossfade` at the root |

**Reduced motion is honoured by every primitive.** `src/hooks/useReducedMotion.ts`
reads `AccessibilityInfo` and keeps itself in sync through a change listener; each
primitive snaps its duration to **zero** when the preference is on, so the final
value lands in the same frame. Motion in Knot is decoration — a press
confirmation, a fade, a dissolve — and none of it carries information the layout
does not already carry, so switching it off is safe.

The earlier motion pass (KNOT-020h) still consumes its own tokens: **The Thread**
(the 1.5pt line that draws itself, now in the primary ink), the overlay push/pop
slide in `App.tsx` (`enter`/`exit` at `durations.screen`), the `micro` pulse on a
tapped `ReactionBar` pill, the chip's fill and icon spring, and the two-step
`micro` bounce on the active `TabBar` glyph.

## Rules

- **Monochrome discipline** — there is no accent. Emphasis comes from weight,
  scale and the ink ramp (`text.primary` → `text.tertiary`), never from a colour.
  The only chromatic inks are `state.danger`, `state.success` and `state.warning`,
  and each is a signal (an error, a confirmation, a caution), never decoration.
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

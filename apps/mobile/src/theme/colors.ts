/**
 * Semantic colour tokens — the roles the UI paints with.
 *
 * Each role maps onto a raw value from `palette.ts`, so a screen asks for
 * `colors.text.secondary` rather than a hex. This indirection is what makes the
 * palette swappable: {@link getColors} resolves a whole role set per theme and
 * caches it, so `colors` identity is stable for as long as the theme is.
 *
 * Both themes are live from KNOT-020i. Components do NOT import the static
 * `colors` object any more — they read the active set from `useTheme()` (or
 * build their stylesheets with `useThemedStyles`), because a stylesheet created
 * at module scope would freeze whichever palette was active when the module was
 * first loaded.
 *
 * ## Mapping the dark palette
 *
 * The dark palette in `tokens.json` names its indigo roles differently from the
 * light one — it has `indigoText`/`indigoFill`/`indigoOnTint` where light has
 * `indigo600/700/500` — because in a dark theme the accent used as *ink* and the
 * accent used as a *fill* need different luminances. {@link activePalette}
 * resolves both into the same role names, so every screen keeps asking for
 * `brand.primary`:
 *
 * - **`brand.primary`** is the accent as ink AND as a fill. Light uses
 *   `indigo600`; dark uses `indigoText` (a light lavender), which is legible as
 *   text on the dark canvas and a compliant fill under dark `text.onBrand`.
 * - **`brand.pressed`** is the pressed state. Light darkens (`indigo700`); dark
 *   BRIGHTENS (`indigoOnTint`), the usual dark-theme affordance.
 *
 * ## Legacy aliases (deprecated)
 *
 * Before KNOT-020a the app was dark-first and `colors` exposed a different shape
 * (`brand.purple`, `bg.primary`, `border.subtle`, `gradient.*`, `state.error`,
 * …). Screens are NOT rewritten until their individual tasks (KNOT-020c onward),
 * so those old keys are kept here as **deprecated aliases** mapped onto the
 * nearest new palette value — the approach the brief recommends (option a). This
 * keeps every existing import resolving while the screens migrate to the new
 * semantic tokens. Expect minor visual shifts (the canvas flips to white) until
 * then; see docs/DESIGN_SYSTEM.md.
 *
 * Do not use the deprecated keys in new code.
 */
import { palette } from './palette';

/** The two themes the app can render. */
export type ThemeName = 'light' | 'dark';

/**
 * A palette flattened into the names the role map below reads.
 *
 * Both palettes resolve into this shape, so the role map itself is written once.
 * The three `indigo*` names are the RESOLVED accents for the theme — see the
 * brand notes in the file header.
 */
type ActivePalette = {
  readonly canvas: string;
  readonly paper: string;
  readonly sheet: string;
  readonly sunken: string;
  readonly line: string;
  readonly ink900: string;
  readonly ink700: string;
  readonly ink500: string;
  readonly ink400: string;
  readonly indigo600: string;
  readonly indigo700: string;
  readonly indigo500: string;
  readonly indigo100: string;
  readonly indigo50: string;
  readonly heritageText: string;
  readonly heritageFill: string;
  readonly wonderText: string;
  readonly wonderFill: string;
  readonly danger: string;
  readonly divider: string;
};

/**
 * The `border.divider` role has no swatch in the design reference: the reference
 * draws the attribution rule with a colour it never names as a token. The light
 * value is the reference's field-border grey; the dark value is one step lighter
 * than the dark hairline, so the two rules stay distinguishable there too.
 * Defining both here keeps the role owned by this layer, as it was before
 * (KNOT-023a).
 */
const DIVIDER = { light: '#D5D9E0', dark: '#2E3442' } as const;

/**
 * Flattens the raw palette for one theme into {@link ActivePalette}.
 *
 * The dark palette is deliberately NOT merged over the light one: every role is
 * named explicitly, so adding a swatch to one theme can never silently inherit
 * from the other.
 */
function activePalette(theme: ThemeName): ActivePalette {
  if (theme === 'light') {
    const p = palette.light;
    return { ...p, sheet: p.canvas, divider: DIVIDER.light };
  }

  const p = palette.dark;
  return {
    canvas: p.canvas,
    paper: p.paper,
    // A sheet floats over a dimmed backdrop, so it is lifted one step off the
    // canvas rather than sharing it — the one dark-only surface in the design.
    sheet: p.sheet,
    sunken: p.sunken,
    line: p.line,
    ink900: p.ink900,
    ink700: p.ink700,
    ink500: p.ink500,
    ink400: p.ink400,
    indigo600: p.indigoText,
    indigo700: p.indigoOnTint,
    indigo500: p.indigoText,
    indigo100: p.indigo100,
    indigo50: p.indigo50,
    heritageText: p.heritageText,
    heritageFill: p.heritageFill,
    wonderText: p.wonderText,
    wonderFill: p.wonderFill,
    danger: p.danger,
    divider: DIVIDER.dark,
  };
}

/** Builds the full role set from a flattened palette. */
function buildColors(active: ActivePalette) {
  return {
    // --- Semantic tokens (new) -------------------------------------------------
    bg: {
      canvas: active.canvas,
      paper: active.paper,
      /**
       * A surface that floats over a scrim — a bottom sheet or a modal card. In
       * light it IS the canvas (the sheet already reads as lifted against the
       * scrim); in dark it is one step lighter so it does not disappear into the
       * dimmed backdrop behind it.
       */
      sheet: active.sheet,
      sunken: active.sunken,
      /**
       * The band of page that shows between two full-width blocks — the feed's
       * story-to-story separator (KNOT-032-fix1).
       *
       * It is a SURFACE, not a rule, so it is not `border.hairline`'s role even
       * though it paints that neutral's value: the separator has to be a visible
       * band (about 8pt) rather than a 1px line, and `sunken` is too close to the
       * white canvas to read at that size. The value therefore comes from the same
       * palette swatch the hairline uses, so the two never drift apart, and dark
       * parity comes for free when the palette flips (KNOT-020i).
       */
      separator: active.line,
      /** @deprecated use `bg.canvas`. */
      primary: active.canvas,
      /** @deprecated use `bg.sunken`. */
      secondary: active.sunken,
      /** @deprecated use `bg.paper`. */
      surface: active.paper,
      /** @deprecated no inverse surface in light; a dark block. */
      inverse: active.ink900,
    },
    text: {
      primary: active.ink900,
      secondary: active.ink700,
      tertiary: active.ink500,
      disabled: active.ink400,
      /**
       * Text (or an icon) drawn on top of a `brand.primary` fill — a primary
       * button, the elevated Create tab. It is the canvas colour so it reads as
       * white while the app is light-first (KNOT-020b); it is a semantic role, not
       * a raw hex, so the no-hardcoded-hex rule holds.
       */
      onBrand: active.canvas,
      /** @deprecated use `text.primary`. */
      inverse: active.ink900,
      /** @deprecated use `brand.primary`. */
      brand: active.indigo600,
    },
    border: {
      hairline: active.line,
      /**
       * The line drawn between two blocks *inside* one entry — the attribution
       * header and the author block — where a hairline would read as the entry's
       * own edge. It is one step darker than `hairline` so the two never read as
       * the same rule when they appear near each other (KNOT-023a).
       *
       * It has no palette swatch in the design reference; like `scrim.overlay`, it
       * is a role this layer owns so the value stays defined in one place, per
       * theme (see `DIVIDER` above).
       */
      divider: active.divider,
      /** @deprecated use `border.hairline`. */
      subtle: active.line,
      /** @deprecated use `border.hairline`. */
      default: active.ink400,
    },
    brand: {
      primary: active.indigo600,
      pressed: active.indigo700,
      focus: active.indigo500,
      tint: active.indigo100,
      surface: active.indigo50,
      /** @deprecated use `brand.primary`. */
      purple: active.indigo600,
      /** @deprecated no magenta in the new palette; use `brand.primary`. */
      magenta: active.indigo600,
      /** @deprecated use `brand.focus`. */
      blue: active.indigo500,
    },
    pillar: {
      heritage: { text: active.heritageText, fill: active.heritageFill },
      wonder: { text: active.wonderText, fill: active.wonderFill },
    },
    state: {
      danger: active.danger,
      /** @deprecated use `state.danger`. */
      error: active.danger,
      /** @deprecated not in the new palette; retained legacy value. */
      success: '#16A34A',
      /** @deprecated not in the new palette; retained legacy value. */
      warning: '#D97706',
    },
    scrim: {
      /**
       * The translucent navy drawn behind a small white-on-media chip (a media
       * count, a "Video" tag). It is a scrim, not a surface: it is only ever
       * painted over an image or a video placeholder, never on the canvas
       * (KNOT-020c-fix).
       *
       * It lives here rather than as a literal on a screen so the value stays the
       * one place an overlay colour is defined.
       */
      overlay: 'rgba(15, 23, 42, 0.6)',
    },

    // --- Deprecated top-level aliases (KNOT-020a) ------------------------------

    /** @deprecated a dark scrim behind modal sheets; unchanged by the new design. */
    overlay: 'rgba(15, 23, 42, 0.7)',

    /** @deprecated gradient tokens are retired — see the no-UI-gradients rule. */
    gradient: {
      primaryStart: active.indigo600,
      primaryEnd: active.indigo500,
      accentStart: active.indigo500,
      accentEnd: active.indigo600,
      warmStart: active.heritageText,
      warmEnd: active.danger,
    },
  };
}

/** The complete semantic role set for one theme. */
export type ThemeColors = ReturnType<typeof buildColors>;

/**
 * Built role sets, one per theme.
 *
 * Cached so the object identity is stable: `useThemedStyles` memoises a
 * stylesheet per colours object, and the context value compares by identity, so
 * a fresh object per render would rebuild every stylesheet in the app.
 */
const built = new Map<ThemeName, ThemeColors>();

/**
 * Resolves the semantic roles for a theme.
 *
 * This is the one place the active palette is chosen. Components reach it
 * through `useTheme()`; anything that must work without a provider (a pure
 * helper, a test) can call it directly.
 */
export function getColors(theme: ThemeName): ThemeColors {
  const cached = built.get(theme);
  if (cached !== undefined) {
    return cached;
  }

  const resolved = buildColors(activePalette(theme));
  built.set(theme, resolved);
  return resolved;
}

/**
 * The LIGHT role set, for anything that renders outside the provider.
 *
 * This is the pre-KNOT-020i export and is kept as the documented fallback: a
 * component used without `ThemeProvider` (a test, a story, a preview) still gets
 * a complete, correct set of roles instead of crashing. Screens must NOT import
 * it — they read the active theme from `useTheme()`.
 */
export const colors = getColors('light');

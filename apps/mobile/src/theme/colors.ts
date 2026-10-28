/**
 * Semantic colour tokens — the roles the UI paints with.
 *
 * Each role maps onto a raw value from `palette.ts`, so a screen asks for
 * `colors.text.secondary` rather than a hex. This indirection is what makes the
 * palette swappable: {@link getColors} resolves a whole role set per mode and
 * caches it, so `colors` identity is stable for as long as the mode is.
 *
 * ## The two modes (KNOT-020k)
 *
 * Knot is monochrome. Both modes are equal citizens, and neither is derived from
 * the other: `palette.light` and `palette.dark` name the same set of values, so a
 * role is resolved by mode rather than by a transformation. The indigo design
 * reference is retired for colour (KNOT-ADR-096).
 *
 * The canonical roles are the brief's contract:
 *
 *   bg.canvas, bg.sunken, bg.raised, bg.scrim
 *   text.primary, text.body, text.secondary, text.tertiary, text.inverse
 *   border.hairline, border.strong
 *   brand.primary, brand.pressed, brand.tint
 *   pillar.wonder, pillar.heritage
 *   state.danger, state.success, state.warning
 *
 * Some roles the app already paints with are kept beside them, because they name
 * a surface or an ink the contract does not: `bg.separator`, `bg.theater`,
 * `text.onBrand`, `text.onMedia`, `text.disabled`, `border.divider`,
 * `scrim.overlay`, `scrim.textShadow`. They resolve to the same monochrome values.
 *
 * ## Legacy aliases (deprecated)
 *
 * Before KNOT-020a the app exposed a different shape (`brand.purple`,
 * `bg.primary`, `border.subtle`, `gradient.*`, `state.error`, …). Those keys are
 * kept as **deprecated aliases** mapped onto the nearest canonical role so every
 * existing import keeps resolving; a new screen must not use them.
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
  readonly sunken: string;
  readonly raised: string;
  readonly scrim: string;
  readonly inkPrimary: string;
  readonly inkBody: string;
  readonly inkSecondary: string;
  readonly inkTertiary: string;
  readonly inkInverse: string;
  readonly line: string;
  readonly lineStrong: string;
  readonly brand: string;
  readonly brandPressed: string;
  readonly brandTint: string;
  readonly danger: string;
  readonly success: string;
  readonly warning: string;
};

/**
 * The ink drawn over media, on a `scrim.overlay` chip or disc.
 *
 * It is white in BOTH modes. The scrim over a photo is black in both modes, so
 * the ink on it must stay the same light value; unlike `text.inverse`, which is
 * the canvas's opposite and therefore flips.
 */
const ON_MEDIA = '#FFFFFF' as const;

/**
 * The media viewer's backdrop, and the surface behind full-screen video.
 *
 * It is black in BOTH modes: a photo viewer is a theatre, and a white canvas
 * behind a photograph distorts how the image reads. Like `text.onMedia`, it is a
 * role this layer owns because no palette value means "the room is dark"
 * (KNOT-020k).
 */
const THEATER = '#000000' as const;

/**
 * The shadow drawn under text on a media scrim.
 *
 * Text sits on a translucent scrim over an arbitrary photo or video frame, so a
 * light glyph can still land on a light area of the image. A tight, low-alpha
 * shadow separates the two without reading as an effect (KNOT-020j).
 */
const MEDIA_TEXT_SHADOW = 'rgba(0, 0, 0, 0.55)' as const;

/**
 * Resolves the raw values for one mode.
 *
 * Both modes name the same keys, so this is a lookup rather than a translation:
 * a value added to one mode cannot silently inherit from the other.
 */
function activePalette(theme: ThemeName): ActivePalette {
  return theme === 'dark' ? palette.dark : palette.light;
}

/** Builds the full role set from one mode's raw values. */
function buildColors(active: ActivePalette) {
  return {
    // --- Canonical semantic roles (KNOT-020k) ----------------------------------
    bg: {
      /** The page behind everything: white in light, black in dark. */
      canvas: active.canvas,
      /** A recessed surface — a field, a well, a placeholder tile. */
      sunken: active.sunken,
      /**
       * A surface that floats above the canvas — a sheet, a modal card, a raised
       * panel. In light it is the canvas value and separation comes from the
       * scrim behind it or a hairline; in dark it is one step off the canvas so
       * it does not disappear into the black (KNOT-020k).
       */
      raised: active.raised,
      /**
       * The translucent veil behind a modal, a sheet or a media chip. Black at
       * half strength in light, heavier in dark, because a dark backdrop needs
       * more separation from a black canvas (KNOT-020k).
       */
      scrim: active.scrim,
      /**
       * The neutral that parts two full-width blocks — the feed's story-to-story
       * rule and the rule under its tab row (KNOT-020k-fix19). It paints the same
       * value as `border.hairline`, so the rules can never drift apart.
       */
      separator: active.line,
      /**
       * The full-screen media viewer's backdrop. Black in both modes, so a
       * photograph or video is shown against the same room whatever the mode is
       * (KNOT-020k).
       */
      theater: THEATER,
      /** @deprecated use `bg.raised`. */
      paper: active.raised,
      /** @deprecated use `bg.raised`. */
      sheet: active.raised,
      /** @deprecated use `bg.canvas`. */
      primary: active.canvas,
      /** @deprecated use `bg.sunken`. */
      secondary: active.sunken,
      /** @deprecated use `bg.raised`. */
      surface: active.raised,
      /** @deprecated no inverse surface; use `bg.sunken`. */
      inverse: active.inkPrimary,
    },
    text: {
      /** The mode's primary ink: black in light, white in dark. */
      primary: active.inkPrimary,
      /** Reading ink: one step softer than the primary, for story bodies. */
      body: active.inkBody,
      /** Supporting ink: timestamps, meta lines, secondary labels. */
      secondary: active.inkSecondary,
      /** The quietest ink: overlines, placeholders, decorative glyphs. */
      tertiary: active.inkTertiary,
      /** Ink that sits on the canvas's opposite — a brand fill, a scrim. */
      inverse: active.inkInverse,
      /** A disabled control's ink. */
      disabled: active.lineStrong,
      /**
       * Text (or an icon) drawn on top of a `brand.primary` fill — a primary
       * button, the elevated Create tab. It is the inverse ink, so it reads
       * white on black in light mode and black on white in dark mode
       * (KNOT-020b, KNOT-020k).
       */
      onBrand: active.inkInverse,
      /**
       * Text and glyphs drawn over media — a video timestamp, a "Video" tag, a
       * multi-media count, a play or mute glyph. White in both modes because it
       * always sits on the black scrim (KNOT-020j).
       */
      onMedia: ON_MEDIA,
      /** @deprecated use `brand.primary`. */
      brand: active.brand,
    },
    border: {
      /** The default 1pt rule: a horizontal separator or a field's edge. */
      hairline: active.line,
      /** A heavier rule, and the boundary of a control that must read as one. */
      strong: active.lineStrong,
      /**
       * The line drawn between two blocks *inside* one entry — the attribution
       * header and the author block — where a hairline would read as the entry's
       * own edge. It is one step heavier than `hairline` so the two never read as
       * the same rule when they appear near each other (KNOT-023a).
       */
      divider: active.lineStrong,
      /** @deprecated use `border.hairline`. */
      subtle: active.line,
      /** @deprecated use `border.strong`. */
      default: active.lineStrong,
    },
    brand: {
      /** The brand's ink and fill: black in light, white in dark. */
      primary: active.brand,
      /** The pressed state: one step off the brand (light darkens, dark dims). */
      pressed: active.brandPressed,
      /** A quiet brand wash — a selected row, a held chip. */
      tint: active.brandTint,
      /** @deprecated no separate focus ring; use `brand.primary`. */
      focus: active.brand,
      /** @deprecated use `brand.tint`. */
      surface: active.brandTint,
      /** @deprecated use `brand.primary`. */
      purple: active.brand,
      /** @deprecated use `brand.primary`. */
      magenta: active.brand,
      /** @deprecated use `brand.primary`. */
      blue: active.brand,
    },
    pillar: {
      /**
       * A pillar is no longer a colour: both resolve to the mode's primary ink,
       * so a pillar reads as bold text rather than as a tinted category
       * (KNOT-020k, KNOT-ADR-096).
       */
      heritage: active.inkPrimary,
      wonder: active.inkPrimary,
    },
    state: {
      /** Destructive actions and errors. */
      danger: active.danger,
      /** Confirmation — reserved for a success affordance. */
      success: active.success,
      /** Caution — reserved for a warning affordance. */
      warning: active.warning,
      /** @deprecated use `state.danger`. */
      error: active.danger,
    },
    scrim: {
      /**
       * The veil behind a modal, a sheet or a small chip drawn over media. It is
       * a scrim, not a surface: it is only ever painted over something else,
       * never on a bare canvas (KNOT-020c-fix). It is the same monochrome value
       * as `bg.scrim`.
       */
      overlay: active.scrim,
      /**
       * The shadow under light text drawn on `overlay`, so the label stays
       * legible even where the media behind it is bright (KNOT-020j).
       */
      textShadow: MEDIA_TEXT_SHADOW,
    },

    // --- Deprecated top-level aliases (KNOT-020a) ------------------------------

    /** @deprecated use `bg.scrim`. */
    overlay: active.scrim,

    /** @deprecated gradient tokens are retired — see the no-UI-gradients rule. */
    gradient: {
      primaryStart: active.brand,
      primaryEnd: active.brand,
      accentStart: active.brand,
      accentEnd: active.brand,
      warmStart: active.brand,
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

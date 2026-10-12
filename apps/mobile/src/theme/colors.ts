/**
 * Semantic colour tokens — the roles the UI paints with.
 *
 * Each role maps onto a raw value from `palette.ts`, so a screen asks for
 * `colors.text.secondary` rather than a hex. This indirection is what makes the
 * palette swappable: the active palette is chosen in exactly one place below.
 *
 * Knot is light-first (KNOT-020a). `colors` reads `palette.light`; the dark
 * palette already exists in `palette.ts` and dark activation is deferred to
 * KNOT-020i.
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

/** The palette the app is currently rendering with. Light until KNOT-020i. */
const active = palette.light;

export const colors = {
  // --- Semantic tokens (new) -------------------------------------------------
  bg: {
    canvas: active.canvas,
    paper: active.paper,
    sunken: active.sunken,
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
} as const;

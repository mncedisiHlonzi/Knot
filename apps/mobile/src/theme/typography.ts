import type { TextStyle } from 'react-native';

/**
 * Font families. Both are bundled locally as `.ttf` assets (KNOT-020a), not
 * loaded from the network, so the app renders them offline. The names must match
 * the linked font files exactly; font linking is a founder-side step
 * (`npx react-native-asset`) followed by a native rebuild — see
 * docs/DESIGN_SYSTEM.md.
 *
 *   ui      — Inter, for everything except hero/display headings
 *   display — Plus Jakarta Sans, reserved for hero and display headings
 */
export const fonts = {
  ui: 'Inter',
  display: 'Plus Jakarta Sans',
  /** @deprecated legacy alias of `ui` (KNOT-020a). */
  primary: 'Inter',
} as const;

/**
 * The five weights in use, as React Native fontWeight strings.
 *
 * `semibold`/`extrabold` follow the lower-case design-token spelling;
 * `semiBold`/`extraBold` are the deprecated legacy spellings, kept so existing
 * screens keep compiling (KNOT-020a).
 */
export const fontWeights = {
  regular: '400',
  medium: '500',
  semibold: '600',
  bold: '700',
  extrabold: '800',
  /** @deprecated use `regular`; retained for the legacy typography composites. */
  light: '300',
  /** @deprecated use `semibold`. */
  semiBold: '600',
  /** @deprecated use `extrabold`. */
  extraBold: '800',
} as const;

/**
 * The composite type scale from docs/design-reference/tokens.json — the twelve
 * named styles the redesigned screens use. Each carries the family, size,
 * line-height, weight and letter-spacing together, so a screen applies one
 * object rather than four separate tokens.
 *
 * `overline` is upper-cased by definition.
 */
export const typeScale = {
  display: {
    fontFamily: fonts.display,
    fontSize: 32,
    lineHeight: 38,
    fontWeight: fontWeights.extrabold,
    letterSpacing: -0.64,
  },
  h1: {
    fontFamily: fonts.display,
    fontSize: 26,
    lineHeight: 32,
    fontWeight: fontWeights.bold,
    letterSpacing: -0.39,
  },
  title: {
    fontFamily: fonts.display,
    fontSize: 20,
    lineHeight: 26,
    fontWeight: fontWeights.bold,
    letterSpacing: -0.2,
  },
  h2: {
    fontFamily: fonts.ui,
    fontSize: 20,
    lineHeight: 26,
    fontWeight: fontWeights.semibold,
    letterSpacing: -0.2,
  },
  h3: {
    fontFamily: fonts.ui,
    fontSize: 17,
    lineHeight: 24,
    fontWeight: fontWeights.semibold,
    letterSpacing: -0.085,
  },
  reading: {
    fontFamily: fonts.ui,
    fontSize: 17,
    lineHeight: 28,
    fontWeight: fontWeights.regular,
    letterSpacing: 0,
  },
  body: {
    fontFamily: fonts.ui,
    fontSize: 15,
    lineHeight: 22,
    fontWeight: fontWeights.regular,
    letterSpacing: 0,
  },
  bodyS: {
    fontFamily: fonts.ui,
    fontSize: 13,
    lineHeight: 19,
    fontWeight: fontWeights.regular,
    letterSpacing: 0,
  },
  button: {
    fontFamily: fonts.ui,
    fontSize: 15,
    lineHeight: 20,
    fontWeight: fontWeights.semibold,
    letterSpacing: 0,
  },
  label: {
    fontFamily: fonts.ui,
    fontSize: 13,
    lineHeight: 16,
    fontWeight: fontWeights.semibold,
    letterSpacing: 0.13,
  },
  overline: {
    fontFamily: fonts.ui,
    fontSize: 11,
    lineHeight: 14,
    fontWeight: fontWeights.semibold,
    letterSpacing: 0.88,
    textTransform: 'uppercase',
  },
  caption: {
    fontFamily: fonts.ui,
    fontSize: 12,
    lineHeight: 16,
    fontWeight: fontWeights.medium,
    letterSpacing: 0.06,
  },
} satisfies Record<string, TextStyle>;

/**
 * Tabular numerals — spread onto any style that renders a number which can
 * change in place (counts, timestamps, reaction tallies, progress). It makes
 * every digit the same width, so the value stops jittering as it updates.
 *
 * Convention: `style={[typeScale.caption, tabularNums]}`. Do NOT apply it to
 * prose; only to numerals.
 */
export const tabularNums: TextStyle = { fontVariant: ['tabular-nums'] };

/**
 * @deprecated Legacy numeric sizes (KNOT-020a). The design pass moves to the
 * composite `typeScale`; these are retained only so screens that still read
 * `fontSizes.*` keep rendering their current sizes until their screen tasks
 * (KNOT-020c onward) migrate them.
 */
export const fontSizes = {
  xs: 11,
  sm: 13,
  base: 15,
  md: 17,
  lg: 20,
  xl: 24,
  '2xl': 32,
} as const;

/** @deprecated legacy line heights paired to `fontSizes` (KNOT-020a). */
export const lineHeights = {
  xs: 16,
  sm: 18,
  base: 22,
  md: 24,
  lg: 28,
  xl: 32,
  '2xl': 40,
} as const;

/**
 * @deprecated Legacy composite text styles (KNOT-020a), kept so screens that
 * import `typography.*` keep compiling. Migrate to `typeScale` in the screen
 * tasks.
 */
export const typography = {
  heading1: {
    fontFamily: fonts.display,
    fontSize: fontSizes['2xl'],
    fontWeight: fontWeights.bold,
    lineHeight: lineHeights['2xl'],
  },
  heading2: {
    fontFamily: fonts.ui,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
    lineHeight: lineHeights.xl,
  },
  heading3: {
    fontFamily: fonts.ui,
    fontSize: fontSizes.lg,
    fontWeight: fontWeights.semiBold,
    lineHeight: lineHeights.lg,
  },
  body: {
    fontFamily: fonts.ui,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.regular,
    lineHeight: lineHeights.base,
  },
  bodySmall: {
    fontFamily: fonts.ui,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.regular,
    lineHeight: lineHeights.sm,
  },
  caption: {
    fontFamily: fonts.ui,
    fontSize: fontSizes.xs,
    fontWeight: fontWeights.regular,
    lineHeight: lineHeights.xs,
  },
  button: {
    fontFamily: fonts.ui,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
    lineHeight: lineHeights.base,
  },
  label: {
    fontFamily: fonts.ui,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.semiBold,
    lineHeight: lineHeights.sm,
  },
} satisfies Record<string, TextStyle>;

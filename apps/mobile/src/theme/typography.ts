import type { TextStyle } from 'react-native';

/**
 * Font families from docs/BRAND.md.
 *
 * Inter is the primary family for everything. Plus Jakarta Sans is the alternate
 * display family, reserved for hero and display headings.
 */
export const fonts = {
  primary: 'Inter',
  display: 'Plus Jakarta Sans',
} as const;

/** The six weights in use: Light through ExtraBold. */
export const fontWeights = {
  light: 300,
  regular: 400,
  medium: 500,
  semiBold: 600,
  bold: 700,
  extraBold: 800,
} as const;

/**
 * A seven-step type scale, each step roughly two points apart, from caption
 * through display.
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

/** Line heights paired to `fontSizes`, tuned for readable body copy. */
export const lineHeights = {
  xs: 16,
  sm: 18,
  base: 22,
  md: 24,
  lg: 28,
  xl: 32,
  '2xl': 40,
} as const;

/** Ready-to-use text styles assembled from the families, weights and scale. */
export const typography = {
  heading1: {
    fontFamily: fonts.display,
    fontSize: fontSizes['2xl'],
    fontWeight: fontWeights.bold,
    lineHeight: lineHeights['2xl'],
  },
  heading2: {
    fontFamily: fonts.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
    lineHeight: lineHeights.xl,
  },
  heading3: {
    fontFamily: fonts.primary,
    fontSize: fontSizes.lg,
    fontWeight: fontWeights.semiBold,
    lineHeight: lineHeights.lg,
  },
  body: {
    fontFamily: fonts.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.regular,
    lineHeight: lineHeights.base,
  },
  bodySmall: {
    fontFamily: fonts.primary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.regular,
    lineHeight: lineHeights.sm,
  },
  caption: {
    fontFamily: fonts.primary,
    fontSize: fontSizes.xs,
    fontWeight: fontWeights.regular,
    lineHeight: lineHeights.xs,
  },
  button: {
    fontFamily: fonts.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
    lineHeight: lineHeights.base,
  },
  label: {
    fontFamily: fonts.primary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.semiBold,
    lineHeight: lineHeights.sm,
  },
} satisfies Record<string, TextStyle>;

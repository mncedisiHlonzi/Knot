/**
 * Corner radius tokens from docs/design-reference/tokens.json.
 *
 * Small radii suit dense or inline elements; larger radii suit cards and sheets.
 * `xxl` is the new token name; `'2xl'` is the deprecated legacy alias (KNOT-020a).
 */
export const radius = {
  xs: 2,
  sm: 4,
  md: 8,
  lg: 12,
  xl: 16,
  xxl: 24,
  pill: 999,
  /** @deprecated use `xxl`. */
  '2xl': 24,
} as const;

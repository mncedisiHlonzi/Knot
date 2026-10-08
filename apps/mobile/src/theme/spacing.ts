/**
 * Spacing scale — the base 8px grid from docs/BRAND.md.
 *
 * `unit` is the base step; every named token is a multiple (4 is the half-step).
 */
export const spacing = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 24,
  '2xl': 32,
  '3xl': 48,
  unit: 8,
} as const;

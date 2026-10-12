/**
 * Spacing scale — the steps from docs/design-reference/tokens.json.
 *
 * `xxl`/`xxxl` are the new token names; `'2xl'`/`'3xl'`/`unit` are the
 * deprecated legacy aliases kept so existing screens keep compiling (KNOT-020a).
 */
export const spacing = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 24,
  xxl: 32,
  xxxl: 48,
  /** @deprecated use `xxl`. */
  '2xl': 32,
  /** @deprecated use `xxxl`. */
  '3xl': 48,
  /** @deprecated legacy base step; use the named tokens directly. */
  unit: 8,
} as const;

/**
 * Layout tokens — screen padding and gutters from the design reference.
 *
 * `screenPaddingFeed` is the edge inset for dense, full-bleed lists (the feed);
 * `screenPaddingReading` is the wider inset for reading flows (story and inquiry
 * detail). `gutter` is the gap between cards; `gutterMedia` is the tighter gap
 * inside a media strip.
 */
export const layout = {
  screenPaddingFeed: 16,
  screenPaddingReading: 24,
  gutter: 16,
  gutterMedia: 12,
} as const;

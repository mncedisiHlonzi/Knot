/**
 * Colour tokens — every value comes from docs/BRAND.md.
 *
 * Brand anchors are flat fills; gradient endpoints are only ever the stops of a
 * gradient and are not interchangeable with the anchors.
 */
export const colors = {
  brand: {
    purple: '#7B3FE4',
    magenta: '#E83EBC',
    blue: '#3B82F6',
  },
  text: {
    primary: '#FFFFFF',
    secondary: '#94A3B8',
    inverse: '#0F172A',
    brand: '#7B3FE4',
  },
  bg: {
    primary: '#0F172A',
    secondary: '#0A0F1E',
    surface: '#1E293B',
    inverse: '#FFFFFF',
  },
  /**
   * A translucent scrim drawn behind a modal sheet. It is a navy tint of
   * `bg.primary` rather than an opaque fill, so the screen stays faintly visible
   * underneath while the sheet reads as the foreground.
   */
  overlay: 'rgba(15, 23, 42, 0.7)',
  border: {
    subtle: '#1E293B',
    default: '#334155',
  },
  gradient: {
    primaryStart: '#6B46FF',
    primaryEnd: '#004BFF',
    accentStart: '#FF3E85',
    accentEnd: '#6B46FF',
    warmStart: '#FF8A3D',
    warmEnd: '#FF3E63',
  },
  state: {
    error: '#DC2626',
    success: '#16A34A',
    warning: '#D97706',
  },
} as const;

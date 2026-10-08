import type { ViewStyle } from 'react-native';

type ElevationStyle = Pick<
  ViewStyle,
  'shadowColor' | 'shadowOffset' | 'shadowOpacity' | 'shadowRadius' | 'elevation'
>;

/**
 * Four elevation levels from docs/BRAND.md, rendered as progressively stronger
 * shadows. Each object carries the iOS shadow props plus the Android `elevation`.
 *
 * 1 — resting cards and inputs; 2 — raised cards and focus states;
 * 3 — floating bars and the compose row; 4 — overlays: modals and sheets.
 */
export const elevation: Record<1 | 2 | 3 | 4, ElevationStyle> = {
  1: {
    shadowColor: '#0F172A',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.08,
    shadowRadius: 2,
    elevation: 1,
  },
  2: {
    shadowColor: '#0F172A',
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.1,
    shadowRadius: 4,
    elevation: 3,
  },
  3: {
    shadowColor: '#0F172A',
    shadowOffset: { width: 0, height: 4 },
    shadowOpacity: 0.12,
    shadowRadius: 8,
    elevation: 6,
  },
  4: {
    shadowColor: '#0F172A',
    shadowOffset: { width: 0, height: 8 },
    shadowOpacity: 0.16,
    shadowRadius: 16,
    elevation: 12,
  },
};

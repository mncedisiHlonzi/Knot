import type { ViewStyle } from 'react-native';

type ElevationStyle = Pick<
  ViewStyle,
  'shadowColor' | 'shadowOffset' | 'shadowOpacity' | 'shadowRadius' | 'elevation'
>;

/**
 * Four elevation levels from docs/design-reference/tokens.json. Each object
 * carries the iOS shadow props (`shadow*`) and the Android `elevation`; React
 * Native applies whichever the platform understands, so both are always set.
 *
 * 1 — resting cards and inputs
 * 2 — raised cards and focus states
 * 3 — floating bars and the compose row (its shadow points up: negative y offset)
 * 4 — overlays: modals and sheets
 */
export const elevation: Record<1 | 2 | 3 | 4, ElevationStyle> = {
  1: {
    shadowColor: '#0F172A',
    shadowOpacity: 0.06,
    shadowOffset: { width: 0, height: 1 },
    shadowRadius: 2,
    elevation: 1,
  },
  2: {
    shadowColor: '#0F172A',
    shadowOpacity: 0.08,
    shadowOffset: { width: 0, height: 2 },
    shadowRadius: 8,
    elevation: 3,
  },
  3: {
    shadowColor: '#0F172A',
    shadowOpacity: 0.12,
    shadowOffset: { width: 0, height: -8 },
    shadowRadius: 24,
    elevation: 8,
  },
  4: {
    shadowColor: '#0F172A',
    shadowOpacity: 0.18,
    shadowOffset: { width: 0, height: 16 },
    shadowRadius: 40,
    elevation: 12,
  },
};

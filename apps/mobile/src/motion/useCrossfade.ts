import { useEffect, useRef, useState } from 'react';
import { Animated, Easing } from 'react-native';

import { useReducedMotion } from '../hooks/useReducedMotion';
import { grey, motion } from '../theme';

/** What {@link useCrossfade} hands the overlay that draws it. */
export type Crossfade = {
  /**
   * The colour to paint over the app while the mode change settles: the canvas of
   * the mode being left. `null` when no crossfade is in flight.
   */
  readonly color: string | null;
  /** The overlay's opacity, animated 1 → 0. */
  readonly opacity: Animated.Value;
};

/**
 * Crossfades the canvas when the app switches between light and dark.
 *
 * A mode switch repaints every surface at once, which reads as a flash rather
 * than a change. Painting the OLD canvas colour over the app and fading it out
 * turns that flash into a dissolve: the new mode is underneath from the first
 * frame, and the old one leaves over `motion.duration.medium`.
 *
 * The overlay only ever covers colour, never content, so it is safe to draw over
 * everything: `ThemeCrossfade` puts it in a `pointerEvents="none"` view above the
 * screens.
 *
 * With reduced motion on the duration is zero, so the overlay is drawn and
 * removed in the same frame — no dissolve, and no flash either, because the
 * removal happens before the next paint.
 *
 * @param isDark whether the ACTIVE mode is dark; the hook watches it flip.
 */
export function useCrossfade(isDark: boolean): Crossfade {
  const reduced = useReducedMotion();
  const opacity = useRef(new Animated.Value(0)).current;
  const previous = useRef(isDark);
  const [color, setColor] = useState<string | null>(null);

  useEffect(() => {
    if (previous.current === isDark) {
      return;
    }

    // The mode being LEFT is the one the overlay must paint, because the tree has
    // already re-rendered in the new mode when this effect runs.
    const leaving = previous.current ? grey[950] : grey[50];
    previous.current = isDark;

    setColor(leaving);
    opacity.setValue(1);

    Animated.timing(opacity, {
      toValue: 0,
      duration: reduced ? 0 : motion.duration.medium,
      easing: Easing.bezier(...motion.easing.standard),
      useNativeDriver: true,
    }).start(({ finished }) => {
      if (finished) {
        setColor(null);
      }
    });
  }, [isDark, opacity, reduced]);

  return { color, opacity };
}

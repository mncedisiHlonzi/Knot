import { useCallback, useEffect, useRef } from 'react';
import { Animated, Easing } from 'react-native';

import { useReducedMotion } from '../hooks/useReducedMotion';
import { motion } from '../theme';

/** What {@link useFade} hands its caller. */
export type Fade = {
  /** The opacity to put on the animated view. */
  readonly opacity: Animated.Value;
  /** Fades the content in from transparent. */
  readonly fadeIn: () => void;
  /**
   * Fades the content out, then calls `done` on completion.
   *
   * `done` is where a caller unmounts the surface (closes a `Modal`, clears its
   * own `visible` state), which is the only way a React Native `Modal` can be
   * faded OUT: its `visible={false}` removes the native window immediately, so an
   * exit animation has to finish first.
   */
  readonly fadeOut: (done?: () => void) => void;
};

/**
 * A fade for overlay content that mounts and unmounts with a surface.
 *
 * Used by the sheets and modals so they arrive softly instead of appearing. The
 * duration is `motion.duration.short` and the curve is `motion.easing.exit`, the
 * decelerating one — an overlay settling into place rather than sliding away.
 *
 * With reduced motion on, both durations snap to zero: the content appears and
 * disappears in the same frame, and `fadeOut` still calls `done`, so the caller's
 * close path is identical whether motion is on or off.
 *
 * @param initialVisible whether to fade in on mount; defaults to true.
 */
export function useFade(initialVisible = true): Fade {
  const reduced = useReducedMotion();
  // Starts transparent so the first frame of a mounted overlay is invisible; the
  // effect below brings it up. A reduced-motion user never sees that frame
  // because the duration is zero.
  const opacity = useRef(new Animated.Value(0)).current;

  const fadeIn = useCallback((): void => {
    Animated.timing(opacity, {
      toValue: 1,
      duration: reduced ? 0 : motion.duration.short,
      easing: Easing.bezier(...motion.easing.exit),
      useNativeDriver: true,
    }).start();
  }, [opacity, reduced]);

  const fadeOut = useCallback(
    (done?: () => void): void => {
      Animated.timing(opacity, {
        toValue: 0,
        duration: reduced ? 0 : motion.duration.short,
        easing: Easing.bezier(...motion.easing.exit),
        useNativeDriver: true,
      }).start(({ finished }) => {
        // An interrupted exit must not unmount the surface the caller just asked
        // to reopen, so `done` runs only for a completed fade.
        if (finished) {
          done?.();
        }
      });
    },
    [opacity, reduced],
  );

  useEffect(() => {
    if (initialVisible) {
      fadeIn();
    }
  }, [fadeIn, initialVisible]);

  return { opacity, fadeIn, fadeOut };
}

import { useCallback, useMemo, useRef } from 'react';
import { Animated, Easing } from 'react-native';

import { useReducedMotion } from '../hooks/useReducedMotion';
import { motion } from '../theme';

/** What {@link usePressScale} hands a pressable. */
export type PressScale = {
  /** The scale to put on the `transform`. Starts at 1. */
  readonly scale: Animated.Value;
  /** Shrinks the control to `motion.scale.press`. */
  readonly onPressIn: () => void;
  /** Returns the control to its resting size. */
  readonly onPressOut: () => void;
};

/**
 * The shared press-scale: the small shrink that makes a control feel touched.
 *
 * It is the one press affordance in the app, so a button, a chip and any future
 * pressable move by the same amount over the same time. The scale and duration
 * come from the motion tokens (`motion.scale.press`, `motion.duration.short`) and
 * the curve from `motion.easing.standard`.
 *
 * The animation runs on the native driver — it only touches `transform` — so a
 * press never blocks the JS thread. When the user prefers reduced motion the
 * duration snaps to zero, which sets the final value in the same frame: the press
 * still registers, it simply does not move.
 *
 * @param enabled set false to keep the caller's own feedback (for example a
 * disabled control); it defaults to true.
 */
export function usePressScale(enabled = true): PressScale {
  const reduced = useReducedMotion();
  const scale = useRef(new Animated.Value(1)).current;

  const animate = useCallback(
    (toValue: number): void => {
      Animated.timing(scale, {
        toValue,
        duration: reduced || !enabled ? 0 : motion.duration.short,
        easing: Easing.bezier(...motion.easing.standard),
        useNativeDriver: true,
      }).start();
    },
    [enabled, reduced, scale],
  );

  return useMemo(
    () => ({
      scale,
      onPressIn: () => animate(motion.scale.press),
      onPressOut: () => animate(1),
    }),
    [animate, scale],
  );
}

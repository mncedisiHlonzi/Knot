import { useEffect, useState } from 'react';
import { AccessibilityInfo } from 'react-native';

/**
 * Reports whether the user has asked the operating system to reduce motion.
 *
 * Motion is decoration in Knot: every animation here is a press confirmation, a
 * fade or a crossfade, and none of them carries information the layout does not
 * already carry. So the honest response to the OS setting is to switch them off
 * rather than to shorten them — the primitives in `src/motion` snap their
 * durations to zero, which lands the element on its final value immediately
 * (KNOT-020k).
 *
 * The initial read is asynchronous (`AccessibilityInfo.isReduceMotionEnabled`),
 * so the hook starts at `false` and corrects itself: a user who prefers reduced
 * motion may see one animation on the very first press after launch, never
 * afterwards. The subscription keeps the value live while the app runs — a person
 * who turns the setting on should not have to restart.
 *
 * A missing API or a rejected read is treated as "motion is allowed", because a
 * failed preference read must never disable the app's feedback.
 */
export function useReducedMotion(): boolean {
  const [reduced, setReduced] = useState(false);

  useEffect(() => {
    let active = true;

    void AccessibilityInfo.isReduceMotionEnabled()
      .then((enabled) => {
        if (active) {
          setReduced(enabled);
        }
      })
      .catch(() => {
        // A failed read leaves the app's motion on, which is the safe default.
      });

    const subscription = AccessibilityInfo.addEventListener('reduceMotionChanged', (enabled) => {
      setReduced(enabled);
    });

    return () => {
      active = false;
      subscription.remove();
    };
  }, []);

  return reduced;
}

/**
 * The duration to hand an animation: the asked-for value, or zero when the user
 * prefers reduced motion.
 *
 * Exported so a caller that drives its own `Animated` call (rather than one of the
 * primitives) can honour the setting without duplicating the rule.
 */
export function useMotionDuration(duration: number): number {
  const reduced = useReducedMotion();
  return reduced ? 0 : duration;
}

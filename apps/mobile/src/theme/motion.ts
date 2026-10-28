/**
 * Motion tokens — the easing curves and durations from
 * `docs/design-reference/tokens.json`.
 *
 * The easings mirror the CSS `cubic-bezier(...)` values in the design reference
 * as four-number tuples, ready for the animation driver that consumes them
 * (react-native-reanimated / Animated). The durations are the millisecond
 * counterparts of the design tokens.
 *
 *   standard — default for most transitions (enter and exit both use it)
 *   enter    — an element appearing (ease-out, decelerating)
 *   exit     — an element leaving (ease-in, accelerating away)
 *
 *   micro  — press feedback, ripples, checkbox ticks
 *   small  — inline toggles, chip selection, tooltips
 *   medium — cards expanding, menus opening
 *   screen — full-screen pushes and modals
 *   max    — the ceiling; a transition must never feel slower than this
 *
 * These tokens are consumed by the motion pass (KNOT-020h): the Thread's loading
 * loop, the overlay push/pop transition in `App.tsx`, and the press, chip, pill
 * and tab micro-interactions. Every duration and curve used by those animations
 * comes from here — the only exceptions are the physics-based springs, which take
 * `speed`/`bounciness` rather than a duration (React Native's own API).
 */
export const motion = {
  easing: {
    standard: [0.2, 0, 0, 1],
    enter: [0.16, 1, 0.3, 1],
    exit: [0.4, 0, 1, 1],
  },
  durations: {
    micro: 110,
    small: 180,
    medium: 240,
    screen: 280,
    max: 320,
  },
  /**
   * The named easings, for a driver that wants a curve by name rather than a
   * tuple (a CSS-backed preview, a native animation spec, a screenshot harness).
   * `easing` above stays the tuple form because that is what `Animated` consumes;
   * the two describe the same three curves (KNOT-020k).
   */
  easingNames: {
    standard: 'easeOutCubic',
    enter: 'easeOut',
    exit: 'easeIn',
  },
  /**
   * The three durations the motion primitives use (KNOT-020k).
   *
   * `durations` above is the original five-step scale the screens animate with;
   * this is the smaller set a shared primitive picks from, so a press, a fade and
   * a crossfade stay in step with each other wherever they are used. Both resolve
   * to milliseconds.
   */
  duration: {
    short: 150,
    medium: 250,
    long: 400,
  },
  /** The scale a pressed control shrinks to (KNOT-020k). */
  scale: {
    press: 0.97,
  },
} as const;

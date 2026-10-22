/**
 * Pure arithmetic for the full-screen media viewer (KNOT-020k).
 *
 * The viewer's gestures and playback are stateful components; the numbers behind
 * them live here so the project's renderer-less test pattern can cover them.
 * Nothing in this module touches React or React Native.
 */

/**
 * Clamps a pager index into `[0, length - 1]`.
 *
 * A caller can ask for an item that no longer exists — a story edited down to
 * fewer media items, or a strip index computed before a re-render — and a viewer
 * must open at a page it can actually show. An empty list resolves to `0`, which
 * the caller reads as "nothing to show"; a non-finite index (a `NaN` from a
 * stale layout) resolves to the first page.
 */
export function clampViewerIndex(index: number, length: number): number {
  if (!Number.isFinite(length) || length <= 0) {
    return 0;
  }
  if (!Number.isFinite(index)) {
    return 0;
  }
  return Math.min(Math.max(Math.round(index), 0), length - 1);
}

/**
 * The played fraction of a video, in `[0, 1]`.
 *
 * A duration of zero (metadata not loaded), or a nonsensical one, yields `0` so
 * the scrub bar rests at the start rather than showing a full or a broken line.
 */
export function viewerProgress(current: number, duration: number): number {
  if (!Number.isFinite(current) || !Number.isFinite(duration) || duration <= 0) {
    return 0;
  }
  return Math.min(Math.max(current / duration, 0), 1);
}

/**
 * The time a touch at `ratio` across the scrub track asks for.
 *
 * It is the inverse of {@link viewerProgress}: the bar is clamped to its own
 * ends, then multiplied back into seconds, so dragging past either edge parks at
 * the start or the end instead of seeking nowhere.
 */
export function scrubSeconds(ratio: number, duration: number): number {
  if (!Number.isFinite(ratio) || !Number.isFinite(duration) || duration <= 0) {
    return 0;
  }
  return Math.min(Math.max(ratio, 0), 1) * duration;
}

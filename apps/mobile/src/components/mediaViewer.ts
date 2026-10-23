/**
 * Pure arithmetic and copy for the full-screen media viewer (KNOT-020k).
 *
 * The viewer's gestures and playback are stateful components; the numbers behind
 * them live here so the project's renderer-less test pattern can cover them.
 * Nothing in this module touches React or React Native.
 */
import { formatDuration } from './feedVideoMeta';

/**
 * How many characters of a story's body the contextual sheet quotes.
 *
 * The sheet clamps the quote to two lines as well, so the window is a guard for a
 * short paragraph on a wide screen rather than the only limit; it is what decides
 * whether the sheet offers "Read more" (KNOT-020k-fix2).
 */
export const VIEWER_SNIPPET_CHARS = 180;

/** The sheet's quotation of a story's body, and whether it is an excerpt. */
export type ViewerSnippet = {
  readonly text: string;
  readonly truncated: boolean;
};

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

/**
 * A story's opening words for the viewer's contextual sheet.
 *
 * Whitespace is collapsed first, because the sheet draws two lines and a body
 * carrying newlines would otherwise waste the window on empty lines. A body that
 * fits is returned whole; a longer one is cut at a word boundary (never inside a
 * word when a space is available in the back half of the window) and closed with
 * an ellipsis, and `truncated` tells the sheet to offer "Read more"
 * (KNOT-020k-fix2).
 */
export function viewerSnippet(
  body: string,
  maxChars: number = VIEWER_SNIPPET_CHARS,
): ViewerSnippet {
  if (typeof body !== 'string') {
    return { text: '', truncated: false };
  }

  const text = body.trim().replace(/\s+/g, ' ');
  if (text === '') {
    return { text: '', truncated: false };
  }

  const limit = Number.isFinite(maxChars) && maxChars > 0 ? Math.floor(maxChars) : 0;
  if (limit === 0 || text.length <= limit) {
    return { text, truncated: false };
  }

  const window = text.slice(0, limit);
  const lastSpace = window.lastIndexOf(' ');
  const clipped = (lastSpace > limit * 0.6 ? window.slice(0, lastSpace) : window).trimEnd();
  return { text: `${clipped}…`, truncated: true };
}

/**
 * A player timestamp that always reads something: "0:00" at the start.
 *
 * {@link formatDuration} deliberately returns `''` for a missing duration, which
 * is right for a badge that hides itself but wrong for a player's running clock —
 * the control bar keeps its geometry, and the reader sees where the clip begins
 * (KNOT-020k-fix2).
 */
export function viewerTimeLabel(seconds: number): string {
  return formatDuration(seconds) || '0:00';
}

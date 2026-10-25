/**
 * Pure arithmetic and copy for the full-screen media viewer (KNOT-020k).
 *
 * The viewer's gestures and playback are stateful components; the numbers behind
 * them live here so the project's renderer-less test pattern can cover them.
 * Nothing in this module touches React or React Native.
 */
import { formatDuration } from './feedVideoMeta';
import type { ViewerMedia, ViewerStory } from './MediaGalleryModal';

/**
 * What the app holds while the full-screen viewer is open.
 *
 * It is the media actually being paged, the page it opened on, and the story the
 * overlay presents — the whole of the viewer's input, so `App` can hand it to a
 * pure function when a request answers late (KNOT-020k-fix16).
 */
export type ViewerState = {
  readonly items: readonly ViewerMedia[];
  readonly index: number;
  readonly story: ViewerStory | null;
};

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
/**
 * The page a viewability report points at.
 *
 * A pager reports the *visible* item's key, but the viewer's active page is an
 * index, so the key has to be resolved against the items on screen. `-1` means
 * "the report named an item this viewer does not hold", which a caller should
 * ignore rather than treat as the first page — a stale key from a list that has
 * since shrunk must never yank the theatre back to page one (KNOT-020k-fix3).
 */
export function viewerIndexForId(ids: readonly string[], key: string): number {
  if (typeof key !== 'string' || key === '') {
    return -1;
  }
  return ids.indexOf(key);
}

/**
 * Swaps a background-read gallery into an already open viewer.
 *
 * A surface that holds only a card preview (the Feed, a place page, the profile
 * wall) opens the viewer at once on the item that was tapped and then reads the
 * story, so swiping reaches the rest. This is the second half of that: it returns
 * the state to install, or `current` untouched when the answer is no longer wanted.
 *
 * Two refusals matter. A viewer that has been **closed** (`current === null`) must
 * stay closed — a late response must never reopen it. And a viewer that is showing
 * a **different story** must not be overwritten, which is how a response for the
 * last tap cannot land on the new one.
 *
 * The **index is deliberately preserved**: it is the page the viewer opened on, and
 * `MediaGalleryModal` keys its stage on it. Recomputing it for the longer array
 * would remount the pager and throw the reader back to the start; instead the stage
 * re-anchors on the same item by url, which now sits at a different index
 * (KNOT-020k-fix16).
 */
export function withHydratedGallery(
  current: ViewerState | null,
  storyId: string,
  gallery: readonly ViewerMedia[],
): ViewerState | null {
  if (current === null || current.story === null || gallery.length === 0) {
    return current;
  }
  if (storyId === '' || current.story.storyId !== storyId) {
    return current;
  }

  return {
    items: gallery,
    index: current.index,
    story: { ...current.story, media: gallery },
  };
}

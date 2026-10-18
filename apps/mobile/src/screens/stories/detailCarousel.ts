/**
 * The pure rules behind the Story Detail media carousel (KNOT-025).
 *
 * The carousel itself is a horizontal paging `ScrollView`; its counter pill and
 * its current-page tracking are arithmetic over the scroll offset, which is the
 * part worth testing without a renderer. Nothing here imports React or React
 * Native, so the rules can be exercised directly.
 */
import type { MediaType } from '../../api/stories';

/**
 * The "n / total" label the counter pill draws, or `''` when there is nothing to
 * count.
 *
 * The index is clamped into range, and the number is one-based because it is a
 * position a reader says out loud ("2 of 10"), not an array index.
 */
export function carouselCounter(index: number, total: number): string {
  if (!Number.isFinite(total) || total <= 0) {
    return '';
  }

  return `${clampIndex(index, total) + 1} / ${Math.floor(total)}`;
}

/**
 * The page index a horizontal paging offset lands on.
 *
 * A paging `ScrollView` reports an offset in points; dividing by the page width
 * gives a fractional page, and the nearest whole page is the one that will settle.
 * A missing or nonsensical width (before the first layout pass) reports page 0
 * rather than a `NaN` index.
 */
export function carouselIndex(offsetX: number, pageWidth: number, total: number): number {
  if (!Number.isFinite(total) || total <= 0) {
    return 0;
  }
  if (!Number.isFinite(pageWidth) || pageWidth <= 0 || !Number.isFinite(offsetX)) {
    return 0;
  }

  return clampIndex(Math.round(offsetX / pageWidth), total);
}

/**
 * What a screen reader hears for a carousel page's position, e.g.
 * "Photograph 2 of 10" or "Video 4 of 10".
 *
 * The media's kind is what a person needs first; the count tells them where they
 * are. A duration is deliberately absent: it is known only after the player loads,
 * and the visible duration badge already carries it.
 */
export function carouselPositionLabel(type: MediaType, index: number, total: number): string {
  const safeTotal = Number.isFinite(total) && total > 0 ? Math.floor(total) : 0;
  const position = clampIndex(index, Math.max(safeTotal, 1)) + 1;
  const noun = type === 'video' ? 'Video' : 'Photograph';

  if (safeTotal === 0) {
    return noun;
  }

  return `${noun} ${position} of ${safeTotal}`;
}

/** Clamps a page index into `[0, total - 1]`, tolerating a non-finite value. */
function clampIndex(index: number, total: number): number {
  if (!Number.isFinite(index)) {
    return 0;
  }

  return Math.min(Math.max(Math.trunc(index), 0), Math.max(total - 1, 0));
}

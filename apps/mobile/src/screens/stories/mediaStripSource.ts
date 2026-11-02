import type { StoryMedia } from '../../api/stories';

/**
 * Which path a media-strip tile draws (KNOT-036-fix4, KNOT-037-fix10).
 *
 * The strip is a 64pt thumbnail row, so it asks for the smallest rendition the
 * server generated rather than the full display image. A video has no cover at
 * all — it autoplays, so the server stopped generating a poster frame — and its
 * tile keeps the sunken placeholder and play disc instead.
 *
 * These live outside the screen so the rule is testable without a renderer, the
 * same shape `detailCarousel.ts` uses for the strip's geometry.
 */

/**
 * The path an image tile draws.
 *
 * The thumbnail wins when the server generated one; otherwise the item's `url` is
 * the only address that exists, and an older server may not even send the field,
 * so a missing or blank value falls back to `url`.
 */
export function stripImagePath(media: Pick<StoryMedia, 'url' | 'thumbnail_url'>): string {
  const thumbnail = media.thumbnail_url;
  if (typeof thumbnail === 'string' && thumbnail !== '') {
    return thumbnail;
  }
  return media.url;
}

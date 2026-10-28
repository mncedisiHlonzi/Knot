import type { StoryMedia } from '../../api/stories';

/**
 * Which path a media-strip tile draws (KNOT-036-fix4).
 *
 * The strip is a 64pt thumbnail row, so it asks for the smallest rendition the
 * server generated rather than the original upload: a 400px JPEG instead of a
 * multi-megabyte photograph, and a poster frame instead of no still at all for a
 * video. Either rendition may be absent — a row written before KNOT-037, or an
 * upload whose decoder or FFmpeg was missing — and then the tile falls back to
 * whatever it can still draw.
 *
 * These live outside the screen so the rule is testable without a renderer, the
 * same shape `detailCarousel.ts` uses for the strip's geometry.
 */

/**
 * The path an image tile draws.
 *
 * The thumbnail wins when the server generated one; otherwise the original is the
 * only address that exists, and an older server may not even send the field, so a
 * missing or blank value falls back to `url`.
 */
export function stripImagePath(media: Pick<StoryMedia, 'url' | 'thumbnail_url'>): string {
  const thumbnail = media.thumbnail_url;
  if (typeof thumbnail === 'string' && thumbnail !== '') {
    return thumbnail;
  }
  return media.url;
}

/**
 * The path a video tile draws as its poster, or `undefined` for the placeholder.
 *
 * `undefined` rather than `url` is deliberate: the original is an MP4, which an
 * `Image` cannot draw, so a video without a poster frame keeps the sunken
 * placeholder and its play disc instead of a broken image.
 */
export function stripPosterPath(media: Pick<StoryMedia, 'poster_url'>): string | undefined {
  const poster = media.poster_url;
  if (typeof poster === 'string' && poster !== '') {
    return poster;
  }
  return undefined;
}

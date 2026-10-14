/**
 * How a list frames a piece of media.
 *
 * The server reports a media item's pixel dimensions alongside its kind and URL
 * (`first_media_width` / `first_media_height`, KNOT-020g-fix5). When it does, the
 * frame takes the media's own width:height ratio, so a portrait photo renders
 * tall and a landscape one renders wide, and the media is shown whole rather than
 * cropped into a fixed window.
 *
 * The dimensions are nullable because they are client-reported: an upload from a
 * picker that could not supply them, or a story uploaded before the columns
 * existed, carries none. That is why the fallback is a named constant rather than
 * a value buried in a screen — the feed, a place page, and the profile wall all
 * frame media, and they must agree on the ratio they use when the shape is
 * unknown.
 *
 * It is deliberately free of any import: a pure rule about two numbers, testable
 * on its own and usable by a component and a screen alike.
 */

/** The ratio a frame falls back to when the media's dimensions are unknown. */
export const FALLBACK_MEDIA_ASPECT_RATIO = 16 / 9;

/**
 * The aspect ratio (width / height) to frame media with.
 *
 * Returns the media's own ratio when both dimensions are known positive numbers,
 * and `FALLBACK_MEDIA_ASPECT_RATIO` otherwise — including when only one of the two
 * is known, because half a pair cannot describe a shape, and when a dimension is
 * zero or negative, which is not a shape either.
 *
 * A caller pairs this with a resize mode that does not crop (`contain`), so an
 * unknown shape is letterboxed inside the fallback frame rather than cut into it.
 */
export function mediaAspectRatio(width?: number | null, height?: number | null): number {
  if (typeof width === 'number' && typeof height === 'number' && width > 0 && height > 0) {
    return width / height;
  }

  return FALLBACK_MEDIA_ASPECT_RATIO;
}

/**
 * Whether the server told the client what shape the media is.
 *
 * A caller that wants to distinguish "framed to the media's own ratio" from
 * "framed to the fallback" asks this rather than re-deriving the guard.
 */
export function hasMediaDimensions(width?: number | null, height?: number | null): boolean {
  return typeof width === 'number' && typeof height === 'number' && width > 0 && height > 0;
}

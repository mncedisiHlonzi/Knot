/**
 * How a list frames a piece of media.
 *
 * The server reports a media item's pixel dimensions alongside its kind and URL
 * (`first_media_width` / `first_media_height`, KNOT-020g-fix5). When it does, the
 * frame takes the media's own width:height ratio, so a portrait photo renders
 * tall and a landscape one renders wide, and the media is shown whole rather than
 * cropped into a fixed window.
 *
 * The server does not always know the shape. Its dimensions are client-reported
 * and the upload pipeline cannot measure everything — a video asset is the common
 * case, because the picker reports only its duration. So a frame can learn the
 * shape two ways: from the payload before the bytes are fetched, or from the
 * media's own natural size once it has loaded. `resolveMediaAspectRatio` settles
 * which of the two to use, and `useMediaAspectRatio` (its React counterpart, in
 * `useMediaAspectRatio.ts`) holds the on-load result across renders.
 *
 * It is deliberately free of any import: a pure rule about four numbers, testable
 * on its own and usable by a component and a screen alike. The hook lives
 * elsewhere so this module can stay that way.
 */

/**
 * The ratio a frame takes before the media has reported its shape.
 *
 * A square, chosen because it is the one frame that is neither wide nor tall: the
 * media fills it (`cover`), so it is a neutral placeholder rather than a promise
 * about the shape that the load may not keep. It is replaced the moment the media
 * reports its natural size (KNOT-020g-fix7).
 */
export const PLACEHOLDER_MEDIA_ASPECT_RATIO = 1;

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
 * A caller frames the media with this ratio and fits it with `cover`, so the media
 * always fills its frame. It is the last resort, for a frame that has no other
 * source of shape — `resolveMediaAspectRatio` prefers a real measurement and
 * reports "unknown" rather than this constant, so that a frame can wait for the
 * media instead of guessing (KNOT-020g-fix6).
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

/**
 * The aspect ratio to frame media with, from whichever source knows its shape.
 *
 * The payload comes first: the server's dimensions arrive with the list, so the
 * frame is already the right shape on the first paint and the media never has to
 * settle into place. Only when the payload cannot describe the media — no columns
 * on a legacy row, or a client that could not measure the file, which is the
 * common case for a video — does the natural size read off the loaded media
 * decide the frame.
 *
 * `null` means neither source has a shape yet: the caller draws its placeholder
 * frame and waits for `onLoad` to report one. That is deliberately different from
 * `mediaAspectRatio`'s fallback, which is a fixed guess — guessing here would
 * reinstate exactly the crop the natural-ratio rule exists to remove
 * (KNOT-020g-fix7).
 */
export function resolveMediaAspectRatio(
  payloadWidth?: number | null,
  payloadHeight?: number | null,
  loadedWidth?: number | null,
  loadedHeight?: number | null,
): number | null {
  if (hasMediaDimensions(payloadWidth, payloadHeight)) {
    return mediaAspectRatio(payloadWidth, payloadHeight);
  }

  if (hasMediaDimensions(loadedWidth, loadedHeight)) {
    return mediaAspectRatio(loadedWidth, loadedHeight);
  }

  return null;
}

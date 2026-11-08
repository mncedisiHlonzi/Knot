/**
 * How a list frames a piece of media.
 *
 * The server reports a media item's pixel dimensions alongside its kind and URL
 * (`first_media_width` / `first_media_height`, KNOT-020g-fix5). When it does, the
 * frame takes the media's own width:height ratio, so a portrait photo renders tall
 * and a landscape one renders wide, and the media is shown whole rather than
 * cropped into a fixed window. The frame is **capped at 4:5**, so a very tall
 * portrait is cropped to a sane list height instead of running off the page
 * (`MAX_PORTRAIT_ASPECT_RATIO`, KNOT-020g-fix12).
 *
 * The rule a surface draws with is `reservedMediaAspectRatio`: the shape is settled
 * from the payload **before** the media is fetched and is never revised, so a card
 * or carousel page is exactly as tall on its first paint as on its last. That is
 * what stops the Feed jumping and the Story Detail pushing its text around while a
 * clip loads (KNOT-048-fix2). Media the payload cannot describe — a video, whose
 * picker usually reports only its duration — takes `RESERVED_MEDIA_ASPECT_RATIO`,
 * the design's canonical 4:5 frame, and keeps it.
 *
 * `resolveMediaAspectRatio` is the older "settle from the media's own load" rule.
 * It is kept because it answers a different question — what shape IS this file —
 * which a caller that is not laying out a list (the full-screen viewer) may want;
 * it must **not** be used to size a frame in a scrolling list.
 *
 * It is deliberately free of any import: a pure rule about four numbers, testable
 * on its own and usable by a component and a screen alike.
 */

/**
 * The ratio a frame takes before the media has reported its shape.
 *
 * A square, chosen because it is the one frame that is neither wide nor tall. It
 * is what `resolveMediaAspectRatio`'s callers drew while they waited for a load;
 * `reservedMediaAspectRatio` deliberately does not use it, because a frame that is
 * replaced by a different shape later is a frame that moves the page
 * (KNOT-020g-fix7, KNOT-048-fix2).
 */
export const PLACEHOLDER_MEDIA_ASPECT_RATIO = 1;

/** The ratio a frame falls back to when the media's dimensions are unknown. */
export const FALLBACK_MEDIA_ASPECT_RATIO = 16 / 9;

/**
 * The narrowest ratio a frame may take — the maximum height a frame may have.
 *
 * A frame is the media's own shape, but **uncapped** that means a 9:16 phone video
 * renders a screen-and-a-half tall in a feed, so one entry pushes every other off
 * the page. 4:5 (Instagram's portrait cap) is the tallest shape a list frame takes:
 * a portrait media taller than 4:5 is framed at 4:5 and cropped at its top and
 * bottom, which a full-bleed `cover` frame does symmetrically. Landscape media is
 * unaffected, because every landscape ratio is already wider than 4:5.
 *
 * It is a **minimum** ratio and therefore a **maximum** height:
 * `height = width / aspectRatio`, so `4/5` caps the height at `width * 1.25`
 * (KNOT-020g-fix12).
 */
export const MAX_PORTRAIT_ASPECT_RATIO = 4 / 5;

/**
 * The shape a frame reserves when the payload cannot describe the media.
 *
 * The design's canonical media shape: every item in
 * `docs/design-reference/story-detail-v2/media-model.json` is 1080 x 1350, which
 * is exactly 4:5 -- the same ratio `MAX_PORTRAIT_ASPECT_RATIO` already caps every
 * frame at. Reserving it means a card's height is decided **before** a byte of
 * media is fetched and does not depend on what the file turns out to be.
 *
 * That is the whole point. A frame that waits for the media and then resizes drags
 * the whole list with it: the card above changes height, everything below moves,
 * and a person scrolling sees the Feed jump under their thumb. The reserved ratio
 * trades a possible crop (the media fills its frame with `cover`) for a layout that
 * never moves (KNOT-048-fix2).
 */
export const RESERVED_MEDIA_ASPECT_RATIO = MAX_PORTRAIT_ASPECT_RATIO;

/**
 * The aspect ratio (width / height) to frame media with.
 *
 * Returns the media's own ratio when both dimensions are known positive numbers,
 * capped at `MAX_PORTRAIT_ASPECT_RATIO` so no frame is taller than 4:5, and
 * `FALLBACK_MEDIA_ASPECT_RATIO` otherwise — including when only one of the two is
 * known, because half a pair cannot describe a shape, and when a dimension is zero
 * or negative, which is not a shape either.
 *
 * A caller frames the media with this ratio and fits it with `cover`, so the media
 * always fills its frame. It is the last resort, for a frame that has no other
 * source of shape — `resolveMediaAspectRatio` prefers a real measurement and
 * reports "unknown" rather than this constant, so that a frame can wait for the
 * media instead of guessing (KNOT-020g-fix6).
 */
export function mediaAspectRatio(width?: number | null, height?: number | null): number {
  if (typeof width === 'number' && typeof height === 'number' && width > 0 && height > 0) {
    // `max` against the portrait cap: a wider (landscape) ratio is already above
    // it and passes through untouched, while a taller one is lifted to the cap.
    return Math.max(width / height, MAX_PORTRAIT_ASPECT_RATIO);
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

/**
 * The shape a list or carousel frame takes, decided from the payload ALONE.
 *
 * Two differences from `resolveMediaAspectRatio`, both deliberate:
 *
 * 1. it never returns `null`, so a caller always has a frame to draw and never
 *    needs a second "placeholder" constant;
 * 2. it never consults what the media reports once it loads, so the frame a card
 *    paints first is the frame it keeps. That is the fix for the Feed jumping while
 *    scrolling and the Story Detail pushing its text around under the media: both
 *    were the same late resize, the card opening as a square placeholder and
 *    collapsing to the clip's real shape once the player had read the file
 *    (KNOT-048-fix2).
 *
 * The server reports dimensions for media it could measure, which is the common
 * case for a photo; a video's picker usually reports only its duration, so a clip
 * takes `RESERVED_MEDIA_ASPECT_RATIO` and stays there.
 */
export function reservedMediaAspectRatio(
  payloadWidth?: number | null,
  payloadHeight?: number | null,
): number {
  return hasMediaDimensions(payloadWidth, payloadHeight)
    ? mediaAspectRatio(payloadWidth, payloadHeight)
    : RESERVED_MEDIA_ASPECT_RATIO;
}

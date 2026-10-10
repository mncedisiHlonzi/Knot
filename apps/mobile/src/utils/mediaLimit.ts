/**
 * The story-media cap, mirrored from the server.
 *
 * The server is the source of truth (`storymedia.MaxMediaPerStory`, enforced
 * race-safely in `POST /stories/{id}/media`, KNOT-ADR-054). The client keeps the
 * same number so a person is told before an upload that would be refused, and so
 * the picker can limit a gallery selection to the free slots.
 *
 * It is deliberately free of any import: a pure rule about a count, testable on
 * its own and usable by the screen and the picker alike.
 */

/** The hard cap on media items per story. Mirrors the server's value. */
export const MAX_MEDIA_PER_STORY = 10;

/** The message shown when a pick would take a story past the cap. */
export const MEDIA_LIMIT_MESSAGE = `You can add up to ${MAX_MEDIA_PER_STORY} media items per story.`;

/**
 * How many more media items a story with `count` items may accept.
 *
 * A story already at or past the cap reports 0 rather than a negative number, so
 * a caller can pass the result straight to a picker's selection limit.
 */
export function remainingMediaSlots(count: number): number {
  return Math.max(0, MAX_MEDIA_PER_STORY - count);
}

/** Reports whether a story with `count` items may accept another. */
export function canAddMedia(count: number): boolean {
  return remainingMediaSlots(count) > 0;
}

/** The media strip's counter, e.g. "3 / 10". */
export function mediaCountLabel(count: number): string {
  return `${count} / ${MAX_MEDIA_PER_STORY}`;
}

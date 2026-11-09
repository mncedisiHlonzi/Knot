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

/**
 * The most VIDEOS one story may carry (KNOT-052).
 *
 * Photos are cheap to decode and stream from the object store; a clip is not, so
 * the feed is kept from becoming a film reel. This is a client-side product rule
 * — the server has no video-specific cap yet — so it is enforced in the composer
 * and disclosed as such.
 */
export const MAX_VIDEOS_PER_STORY = 3;

/** The message shown when a pick would take a story past the cap. */
export const MEDIA_LIMIT_MESSAGE = `You can add up to ${MAX_MEDIA_PER_STORY} media items per story.`;

/** The message shown when a pick would take a story past the video cap. */
export const VIDEO_LIMIT_MESSAGE = `You can only select up to ${MAX_VIDEOS_PER_STORY} videos per story.`;

/** The two kinds of media a story can carry. */
export type MediaKind = 'image' | 'video';

/** The one field these rules need from a media item. */
export type MediaLike = { readonly type: MediaKind };

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

/** How many videos a story already holding `videos` may still accept. */
export function remainingVideoSlots(videos: number): number {
  return Math.max(0, MAX_VIDEOS_PER_STORY - videos);
}

/** Reports whether a story holding `videos` videos may accept another one. */
export function canAddVideo(videos: number): boolean {
  return remainingVideoSlots(videos) > 0;
}

/** How many of these items are videos. */
export function countVideos(media: readonly MediaLike[]): number {
  return media.reduce((total, item) => (item.type === 'video' ? total + 1 : total), 0);
}

/** The media strip's counter, e.g. "3 / 10". */
export function mediaCountLabel(count: number): string {
  return `${count} / ${MAX_MEDIA_PER_STORY}`;
}

/**
 * The media strip's label, e.g. "Media · 3 of 10 · 2 videos".
 *
 * The video segment is only drawn once there is a video to count: a story of
 * photographs does not need to be told it holds none, and the line stays short
 * enough to read in one glance (KNOT-052).
 */
export function mediaStripLabel(total: number, videos: number): string {
  const head = `Media · ${total} of ${MAX_MEDIA_PER_STORY}`;
  if (videos < 1) {
    return head;
  }
  return `${head} · ${videos} ${videos === 1 ? 'video' : 'videos'}`;
}

/** What a pick produced once both caps had their say. */
export type MediaAdmission<T extends MediaLike> = {
  /** The items that fit, in the order they were picked. */
  readonly accepted: readonly T[];
  /** True when a video was refused because the story already holds its maximum. */
  readonly videoLimitReached: boolean;
  /** True when an item was refused because the story is full. */
  readonly mediaLimitReached: boolean;
};

/**
 * Applies the caps to a pick, keeping the items that still fit.
 *
 * Both caps are checked per item, in the order the picker returned them, so a
 * selection that is partly acceptable is partly kept rather than rejected whole:
 * a fourth video is dropped while the photograph beside it still lands
 * (KNOT-052). A full story stops the walk — no later item can fit either.
 */
export function admitMedia<T extends MediaLike>(
  existing: readonly MediaLike[],
  picked: readonly T[],
): MediaAdmission<T> {
  const accepted: T[] = [];
  let total = existing.length;
  let videos = countVideos(existing);
  let videoLimitReached = false;
  let mediaLimitReached = false;

  for (const item of picked) {
    if (total >= MAX_MEDIA_PER_STORY) {
      mediaLimitReached = true;
      break;
    }
    if (item.type === 'video' && videos >= MAX_VIDEOS_PER_STORY) {
      videoLimitReached = true;
      continue;
    }
    accepted.push(item);
    total += 1;
    if (item.type === 'video') {
      videos += 1;
    }
  }

  return { accepted, videoLimitReached, mediaLimitReached };
}

import type { MediaSource, MediaStatus, Story, StoryMedia } from '../../api/stories';

/**
 * The rules behind a story whose media is still being encoded (KNOT-037-fix8).
 *
 * `POST /stories` and `POST /stories/{id}/media` answer as soon as the row is
 * written: the bytes are generated afterwards by the backend's worker, so the item
 * arrives with `status: 'processing'` and becomes `ready` a few seconds later
 * (KNOT-037-fix7). The app therefore has to do two things the previous pipeline did
 * not need: draw something honest while the bytes do not exist, and ask again until
 * they do.
 *
 * The polling loop lives in `runStoryPolling` rather than inside the hook so it can
 * be driven with a fake clock and a fake API: no timers, no renderer, and the whole
 * "keep asking until it is ready" rule is tested directly. `useStoryPolling` is the
 * thin React wrapper around it.
 */

/** How often a processing story is re-read. */
export const POLL_INTERVAL_MS = 2000;

/**
 * How long to keep asking before giving up.
 *
 * A 20 MiB video transcode takes seconds, not minutes; past a minute something is
 * wrong, and a card that polls for ever would be a battery and network leak. When
 * the deadline passes the placeholder stays and the media simply never appears —
 * a deliberate choice, because the alternative is a spinner that lies.
 */
export const POLL_TIMEOUT_MS = 60_000;

/** What a poll run decided. */
export type StoryPollOutcome =
  /** Every media item is ready (or the story has none) — stop. */
  | 'ready'
  /** An item's processing failed — stop; nothing is coming. */
  | 'failed'
  /** The deadline passed with the item still processing — stop. */
  | 'timeout'
  /** The screen went away, so the loop was cancelled — stop. */
  | 'cancelled';

/** True while at least one media item has no bytes yet. */
export function hasProcessingMedia(media: readonly StoryMedia[] | undefined): boolean {
  return (media ?? []).some((item) => item.status === 'processing');
}

/** True when at least one media item's processing failed. */
export function hasFailedMedia(media: readonly StoryMedia[] | undefined): boolean {
  return (media ?? []).some((item) => item.status === 'failed');
}

/**
 * The status one card should act on, given the media array a payload carries.
 *
 * `processing` wins over `failed` wins over `ready`: a card only ever draws its
 * first item, and while anything at all is still being made the honest answer is
 * "not yet". An absent status is `ready`, which is what a row written before the
 * async pipeline is.
 */
export function mediaStatusOf(media: readonly StoryMedia[] | undefined): MediaStatus | undefined {
  const items = media ?? [];
  if (items.some((item) => item.status === 'processing')) {
    return 'processing';
  }
  if (items.some((item) => item.status === 'failed')) {
    return 'failed';
  }

  return items[0]?.status ?? 'ready';
}

/**
 * The id of the first entry whose media is still processing, or undefined.
 *
 * The feed polls one story at a time: uploads are one at a time in the composer
 * (KNOT-ADR-032), so in practice there is never more than one. When several are
 * processing the first is polled, and the next becomes the target as soon as it is
 * done.
 */
export function firstProcessingStoryId(
  items: readonly { readonly id: string; readonly media?: readonly StoryMedia[] }[],
): string | undefined {
  return items.find((item) => hasProcessingMedia(item.media))?.id;
}

/**
 * Puts a story into a list, newest first, replacing any copy already there.
 *
 * The composer's optimistic entry has to survive a page load that raced it: the
 * feed may finish its first request before or after the story is handed over, and
 * either order must leave exactly one copy of it at the top (KNOT-037-fix8).
 *
 * It also has to survive a poll that runs BEFORE the uploads land. The composer
 * hands the story over as soon as `POST /stories` answers and uploads the media
 * afterwards (KNOT-037-fix12), so the server legitimately reports no media for a
 * few seconds. Taking that answer literally would clear the card's "Processing"
 * state and stop the feed asking again, so the local placeholders are kept until
 * the server acknowledges at least one item.
 */
export function itemsWithStory<
  T extends { readonly id: string; readonly media?: readonly StoryMedia[] },
>(items: readonly T[], story: T | undefined): readonly T[] {
  if (story === undefined) {
    return items;
  }

  const existingIndex = items.findIndex((item) => item.id === story.id);
  if (existingIndex < 0) {
    return [story, ...items];
  }

  const existing = items[existingIndex];
  const awaitingUpload = (story.media ?? []).length === 0 && hasProcessingMedia(existing.media);

  const updated = items.slice();
  updated[existingIndex] = awaitingUpload
    ? ({ ...story, media: existing.media } as T)
    : { ...existing, ...story };
  return updated;
}

/** What the composer picked, in the shape the optimistic story needs. */
export type PendingMediaInput = {
  readonly type: 'image' | 'video';
  readonly mimeType: string;
  readonly source: MediaSource;
  /** Size in bytes, 0 when the picker did not report one. */
  readonly size: number;
  readonly width?: number;
  readonly height?: number;
  /** Video duration in SECONDS, the unit the picker reports. */
  readonly duration?: number;
};

/**
 * The story as it should appear before the server's media rows exist.
 *
 * The composer knows what it uploaded but not yet what the server called it, so the
 * items are local stand-ins: the media type and the dimensions are real (they come
 * from the picker), which is what lets the placeholder take the right shape, and
 * `status: 'processing'` is what tells the card not to fetch anything. They are
 * replaced wholesale by the real rows as soon as the poll returns them
 * (KNOT-037-fix8).
 *
 * `url` is deliberately empty: there is no object to fetch yet, and an empty path
 * is what the card already treats as "no media to draw".
 */
export function withPendingMedia(story: Story, picks: readonly PendingMediaInput[]): Story {
  if (picks.length === 0) {
    return story;
  }

  const media: readonly StoryMedia[] = picks.map((pick, index) => ({
    id: `pending-${story.id}-${index}`,
    media_type: pick.type,
    source: pick.source,
    mime_type: pick.mimeType,
    width: pick.width ?? null,
    height: pick.height ?? null,
    duration_ms: pick.duration === undefined ? null : Math.round(pick.duration * 1000),
    size_bytes: pick.size,
    display_order: index,
    created_at: story.created_at,
    url: '',
    status: 'processing',
  }));

  return {
    ...story,
    media,
    media_count: picks.length,
    first_media_type: picks[0]?.type ?? null,
    first_media_url: null,
    first_media_width: picks[0]?.width ?? null,
    first_media_height: picks[0]?.height ?? null,
  };
}

/** The options one poll run needs. Every dependency is injectable for tests. */
export type StoryPollOptions = {
  /** The story to watch. */
  readonly storyId: string;
  /** Reads the story once. The hook passes `storiesApi.getStory`. */
  readonly fetchStory: (storyId: string) => Promise<Story>;
  /** Called with every fresh copy, so the caller can re-render. */
  readonly onUpdate: (story: Story) => void;
  /** Milliseconds between attempts. Defaults to `POLL_INTERVAL_MS`. */
  readonly intervalMs?: number;
  /** Milliseconds before giving up. Defaults to `POLL_TIMEOUT_MS`. */
  readonly timeoutMs?: number;
  /** The clock. Defaults to `Date.now`. */
  readonly now?: () => number;
  /** Waits between attempts. Defaults to a `setTimeout` promise. */
  readonly sleep?: (ms: number) => Promise<void>;
  /** Checked before and after every wait, so an unmounted screen stops the loop. */
  readonly isCancelled?: () => boolean;
  /**
   * True when media is known to be on its way but the server has not recorded it
   * yet — the composer uploads after it publishes (KNOT-037-fix12).
   *
   * Without this, the first read that finds no media would be read as "nothing was
   * ever attached" and end the loop, which is exactly what happened when the
   * composer stopped waiting for its uploads. Defaults to `false`, so a story that
   * genuinely has no media still finishes on its first answer.
   */
  readonly awaitingMedia?: boolean;
};

/** Waits for `ms` milliseconds. */
function wait(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

/**
 * Asks for a story every `intervalMs` until its media stops processing.
 *
 * The first read happens AFTER one interval, not immediately: the row was just
 * written, so asking at once would only learn what the caller already knows. A
 * failed read is not a verdict — the encode is still running and a network blip
 * says nothing about it — so the loop keeps going until the deadline. It stops
 * early for `ready`, `failed`, or a cancellation, and returns which happened.
 */
export async function runStoryPolling(options: StoryPollOptions): Promise<StoryPollOutcome> {
  const intervalMs = options.intervalMs ?? POLL_INTERVAL_MS;
  const timeoutMs = options.timeoutMs ?? POLL_TIMEOUT_MS;
  const now = options.now ?? ((): number => Date.now());
  const sleep = options.sleep ?? wait;
  const isCancelled = options.isCancelled ?? ((): boolean => false);

  const startedAt = now();

  while (!isCancelled()) {
    await sleep(intervalMs);
    if (isCancelled()) {
      return 'cancelled';
    }

    let story: Story;
    try {
      story = await options.fetchStory(options.storyId);
    } catch {
      // Unreadable right now. The deadline is the only thing that ends this.
      if (now() - startedAt >= timeoutMs) {
        return 'timeout';
      }
      continue;
    }

    options.onUpdate(story);

    const media = story.media ?? [];
    if (options.awaitingMedia === true && media.length === 0) {
      // The upload has not reached the server yet, so there is nothing to judge: a
      // story with no media rows yet is not the same as a story without media.
      if (now() - startedAt >= timeoutMs) {
        return 'timeout';
      }
      continue;
    }

    if (!hasProcessingMedia(media)) {
      return hasFailedMedia(media) ? 'failed' : 'ready';
    }
    if (now() - startedAt >= timeoutMs) {
      return 'timeout';
    }
  }

  return 'cancelled';
}

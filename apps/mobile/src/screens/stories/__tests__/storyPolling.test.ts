import type { MediaStatus, Story, StoryMedia } from '../../../api/stories';
import {
  POLL_INTERVAL_MS,
  POLL_TIMEOUT_MS,
  firstProcessingStoryId,
  hasFailedMedia,
  hasProcessingMedia,
  itemsWithStory,
  mediaStatusOf,
  runStoryPolling,
  withPendingMedia,
} from '../storyPolling';

/**
 * The async media rules (KNOT-037-fix8).
 *
 * The polling loop takes its clock, its timer and its fetcher as arguments, so
 * every "keeps asking" rule is exercised here with fakes: no timers to wait on and
 * no renderer, which is the project's test pattern.
 */

/** One media item, with only the fields these rules read. */
function media(status: MediaStatus, id = 'media-1'): StoryMedia {
  return { id, status } as unknown as StoryMedia;
}

/** A story carrying the given items. */
function storyWith(media: readonly StoryMedia[], id = 'story-1'): Story {
  return { id, media } as unknown as Story;
}

/** A clock and a sleep that advance together, so time passes without waiting. */
function fakeClock(): { now: () => number; sleep: (ms: number) => Promise<void> } {
  let clock = 0;

  return {
    now: () => clock,
    sleep: async (ms: number): Promise<void> => {
      clock += ms;
    },
  };
}

describe('media status helpers', () => {
  it('reports whether anything is still being encoded', () => {
    expect(hasProcessingMedia([media('processing')])).toBe(true);
    expect(hasProcessingMedia([media('ready'), media('processing')])).toBe(true);
    expect(hasProcessingMedia([media('ready')])).toBe(false);
    expect(hasProcessingMedia([])).toBe(false);
    expect(hasProcessingMedia(undefined)).toBe(false);
  });

  it('reports a failed item only when nothing is still running', () => {
    expect(hasFailedMedia([media('failed')])).toBe(true);
    expect(hasFailedMedia([media('ready'), media('failed')])).toBe(true);
    expect(hasFailedMedia([media('ready')])).toBe(false);
    expect(hasFailedMedia(undefined)).toBe(false);
  });

  it('prefers processing, then failed, then ready', () => {
    expect(mediaStatusOf([media('ready'), media('processing')])).toBe('processing');
    expect(mediaStatusOf([media('ready'), media('failed')])).toBe('failed');
    expect(mediaStatusOf([media('ready')])).toBe('ready');
  });

  it('treats an item from an older server, which sends no status, as ready', () => {
    expect(mediaStatusOf([{ id: 'm' } as unknown as StoryMedia])).toBe('ready');
    expect(mediaStatusOf([])).toBe('ready');
  });

  it('finds the first entry whose media is still processing', () => {
    const items = [
      { id: 'a', media: [media('ready')] },
      { id: 'b', media: [media('processing')] },
      { id: 'c', media: [media('processing')] },
    ];

    expect(firstProcessingStoryId(items)).toBe('b');
    expect(firstProcessingStoryId([{ id: 'a', media: [media('ready')] }])).toBeUndefined();
    expect(firstProcessingStoryId([])).toBeUndefined();
  });
});

describe('itemsWithStory', () => {
  const existing = [{ id: 'a' }, { id: 'b' }];

  it('puts a new story at the top of the list', () => {
    expect(itemsWithStory(existing, { id: 'new' })).toEqual([{ id: 'new' }, ...existing]);
  });

  it('replaces a copy already in the list rather than duplicating it', () => {
    const updated = itemsWithStory(existing, { id: 'b', extra: 'fresh' } as { id: string });

    expect(updated).toHaveLength(2);
    expect(updated[1]).toEqual({ id: 'b', extra: 'fresh' });
  });

  it('leaves the list alone when there is nothing to add', () => {
    expect(itemsWithStory(existing, undefined)).toEqual(existing);
  });
});

describe('withPendingMedia', () => {
  const created = {
    id: 'story-1',
    title: 'A story',
    created_at: '2026-11-01T20:00:00.000Z',
    media: [],
  } as unknown as Story;

  it('marks the picks as processing so the card can draw a placeholder', () => {
    const optimistic = withPendingMedia(created, [
      { type: 'video', mimeType: 'video/mp4', source: 'camera', size: 2048, duration: 12.4 },
      {
        type: 'image',
        mimeType: 'image/jpeg',
        source: 'gallery',
        size: 512,
        width: 900,
        height: 1600,
      },
    ]);

    expect(optimistic.media).toHaveLength(2);
    expect(optimistic.media?.[0]).toMatchObject({
      media_type: 'video',
      status: 'processing',
      duration_ms: 12400,
      url: '',
    });
    expect(optimistic.media?.[1]).toMatchObject({
      media_type: 'image',
      status: 'processing',
      width: 900,
      height: 1600,
    });
  });

  it('describes the first item the way the feed card reads it', () => {
    const optimistic = withPendingMedia(created, [
      {
        type: 'image',
        mimeType: 'image/jpeg',
        source: 'gallery',
        size: 512,
        width: 900,
        height: 1600,
      },
    ]);

    expect(optimistic.first_media_type).toBe('image');
    expect(optimistic.first_media_url).toBeNull();
    expect(optimistic.first_media_width).toBe(900);
    expect(optimistic.media_count).toBe(1);
  });

  it('returns the story untouched when nothing was picked', () => {
    expect(withPendingMedia(created, [])).toBe(created);
  });
});

describe('runStoryPolling', () => {
  it('asks again every two seconds until the media is ready', async () => {
    const clock = fakeClock();
    const fetchStory = jest
      .fn()
      .mockResolvedValueOnce(storyWith([media('processing')]))
      .mockResolvedValueOnce(storyWith([media('processing')]))
      .mockResolvedValueOnce(storyWith([media('ready')]));
    const updates: Story[] = [];

    const outcome = await runStoryPolling({
      storyId: 'story-1',
      fetchStory,
      onUpdate: (story) => updates.push(story),
      ...clock,
    });

    expect(outcome).toBe('ready');
    expect(fetchStory).toHaveBeenCalledTimes(3);
    expect(fetchStory).toHaveBeenNthCalledWith(1, 'story-1');
    expect(updates).toHaveLength(3);
    expect(POLL_INTERVAL_MS).toBe(2000);
  });

  it('stops as soon as the server says the item is ready', async () => {
    const clock = fakeClock();
    const fetchStory = jest.fn().mockResolvedValue(storyWith([media('ready')]));

    const outcome = await runStoryPolling({
      storyId: 'story-1',
      fetchStory,
      onUpdate: () => undefined,
      ...clock,
    });

    expect(outcome).toBe('ready');
    expect(fetchStory).toHaveBeenCalledTimes(1);
  });

  it('stops when the item failed: nothing is coming', async () => {
    const clock = fakeClock();
    const fetchStory = jest.fn().mockResolvedValue(storyWith([media('failed')]));

    const outcome = await runStoryPolling({
      storyId: 'story-1',
      fetchStory,
      onUpdate: () => undefined,
      ...clock,
    });

    expect(outcome).toBe('failed');
    expect(fetchStory).toHaveBeenCalledTimes(1);
  });

  it('gives up after a minute of processing', async () => {
    const clock = fakeClock();
    const fetchStory = jest.fn().mockResolvedValue(storyWith([media('processing')]));

    const outcome = await runStoryPolling({
      storyId: 'story-1',
      fetchStory,
      onUpdate: () => undefined,
      ...clock,
    });

    expect(outcome).toBe('timeout');
    expect(POLL_TIMEOUT_MS).toBe(60000);
    // One attempt per interval, until the deadline is reached.
    expect(fetchStory).toHaveBeenCalledTimes(POLL_TIMEOUT_MS / POLL_INTERVAL_MS);
  });

  it('keeps trying through a failed read', async () => {
    const clock = fakeClock();
    const fetchStory = jest
      .fn()
      .mockRejectedValueOnce(new Error('network down'))
      .mockRejectedValueOnce(new Error('still down'))
      .mockResolvedValueOnce(storyWith([media('ready')]));

    const outcome = await runStoryPolling({
      storyId: 'story-1',
      fetchStory,
      onUpdate: () => undefined,
      ...clock,
    });

    expect(outcome).toBe('ready');
    expect(fetchStory).toHaveBeenCalledTimes(3);
  });

  it('stops when the screen goes away', async () => {
    const clock = fakeClock();
    const fetchStory = jest.fn().mockResolvedValue(storyWith([media('processing')]));

    const outcome = await runStoryPolling({
      storyId: 'story-1',
      fetchStory,
      onUpdate: () => undefined,
      isCancelled: () => true,
      ...clock,
    });

    expect(outcome).toBe('cancelled');
    expect(fetchStory).not.toHaveBeenCalled();
  });

  it('stops mid-flight when the screen unmounts between attempts', async () => {
    const clock = fakeClock();
    let calls = 0;
    const fetchStory = jest.fn().mockImplementation(async () => storyWith([media('processing')]));

    const outcome = await runStoryPolling({
      storyId: 'story-1',
      fetchStory,
      onUpdate: () => undefined,
      isCancelled: () => calls >= 2,
      sleep: async (ms: number) => {
        calls += 1;
        clock.sleep(ms);
      },
    });

    expect(outcome).toBe('cancelled');
    expect(fetchStory).toHaveBeenCalledTimes(1);
  });
});

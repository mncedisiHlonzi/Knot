import {
  MAX_MEDIA_PER_STORY,
  MAX_VIDEOS_PER_STORY,
  MEDIA_LIMIT_MESSAGE,
  VIDEO_LIMIT_MESSAGE,
  admitMedia,
  canAddMedia,
  canAddVideo,
  countVideos,
  mediaCountLabel,
  mediaStripLabel,
  remainingMediaSlots,
  remainingVideoSlots,
} from '../mediaLimit';

describe('MAX_MEDIA_PER_STORY', () => {
  it('is the cap the server enforces', () => {
    expect(MAX_MEDIA_PER_STORY).toBe(10);
  });
});

describe('remainingMediaSlots', () => {
  it('reports every slot on an empty story', () => {
    expect(remainingMediaSlots(0)).toBe(MAX_MEDIA_PER_STORY);
  });

  it('counts down as items are added', () => {
    expect(remainingMediaSlots(1)).toBe(9);
    expect(remainingMediaSlots(9)).toBe(1);
  });

  it('reports none at the cap', () => {
    expect(remainingMediaSlots(MAX_MEDIA_PER_STORY)).toBe(0);
  });

  it('never goes negative, so the result is safe as a selection limit', () => {
    expect(remainingMediaSlots(MAX_MEDIA_PER_STORY + 1)).toBe(0);
    expect(remainingMediaSlots(100)).toBe(0);
  });
});

describe('canAddMedia', () => {
  it('allows an item below the cap', () => {
    expect(canAddMedia(0)).toBe(true);
    expect(canAddMedia(MAX_MEDIA_PER_STORY - 1)).toBe(true);
  });

  it('refuses an item at or past the cap', () => {
    expect(canAddMedia(MAX_MEDIA_PER_STORY)).toBe(false);
    expect(canAddMedia(MAX_MEDIA_PER_STORY + 1)).toBe(false);
  });
});

describe('mediaCountLabel', () => {
  it('shows the count against the cap', () => {
    expect(mediaCountLabel(0)).toBe('0 / 10');
    expect(mediaCountLabel(3)).toBe('3 / 10');
    expect(mediaCountLabel(MAX_MEDIA_PER_STORY)).toBe('10 / 10');
  });
});

describe('MEDIA_LIMIT_MESSAGE', () => {
  it('names the cap', () => {
    expect(MEDIA_LIMIT_MESSAGE).toBe('You can add up to 10 media items per story.');
  });
});

describe('the video cap', () => {
  it('allows three clips in one story', () => {
    expect(MAX_VIDEOS_PER_STORY).toBe(3);
  });

  it('counts down independently of the media cap', () => {
    expect(remainingVideoSlots(0)).toBe(3);
    expect(remainingVideoSlots(2)).toBe(1);
    expect(remainingVideoSlots(3)).toBe(0);
    expect(remainingVideoSlots(9)).toBe(0);
  });

  it('refuses a fourth clip', () => {
    expect(canAddVideo(2)).toBe(true);
    expect(canAddVideo(3)).toBe(false);
  });

  it('words the refusal', () => {
    expect(VIDEO_LIMIT_MESSAGE).toBe('You can only select up to 3 videos per story.');
  });
});

describe('countVideos', () => {
  it('counts only the clips', () => {
    expect(countVideos([])).toBe(0);
    expect(
      countVideos([{ type: 'image' }, { type: 'video' }, { type: 'video' }, { type: 'image' }]),
    ).toBe(2);
  });
});

describe('mediaStripLabel', () => {
  it('names the total against the cap', () => {
    expect(mediaStripLabel(0, 0)).toBe('Media · 0 of 10');
    expect(mediaStripLabel(3, 0)).toBe('Media · 3 of 10');
  });

  it('adds the clip count once there is one', () => {
    expect(mediaStripLabel(3, 2)).toBe('Media · 3 of 10 · 2 videos');
    // The label's own plural, because the overline token uppercases it.
    expect(mediaStripLabel(1, 1)).toBe('Media · 1 of 10 · 1 video');
  });
});

/**
 * The rule the composer actually applies to a pick.
 *
 * A pick can be partly acceptable — one mixed gallery selection may carry more
 * clips than the story may hold, beside photographs that still fit — so the
 * admission keeps what fits and names which cap refused the rest (KNOT-052).
 */
describe('admitMedia', () => {
  const image = { type: 'image' as const };
  const video = { type: 'video' as const };

  it('takes everything when nothing is near a cap', () => {
    const picked = [image, video, image];

    expect(admitMedia([], picked)).toEqual({
      accepted: picked,
      videoLimitReached: false,
      mediaLimitReached: false,
    });
  });

  it('refuses a fourth video, and says which cap refused it', () => {
    const existing = [video, video, video];

    const admission = admitMedia(existing, [video]);

    expect(admission.accepted).toEqual([]);
    expect(admission.videoLimitReached).toBe(true);
    expect(admission.mediaLimitReached).toBe(false);
  });

  it('keeps the photographs beside a refused clip', () => {
    // One mixed pick: a fourth clip arrives with two photographs that fit.
    const admission = admitMedia([video, video, video], [video, image, image]);

    expect(admission.accepted).toEqual([image, image]);
    expect(admission.videoLimitReached).toBe(true);
  });

  it('refuses everything once the story holds its media cap', () => {
    const existing = Array.from({ length: MAX_MEDIA_PER_STORY }, () => image);

    const admission = admitMedia(existing, [image, video]);

    expect(admission.accepted).toEqual([]);
    expect(admission.mediaLimitReached).toBe(true);
    expect(admission.videoLimitReached).toBe(false);
  });

  it('fills the last media slots from the front of the pick', () => {
    const existing = Array.from({ length: MAX_MEDIA_PER_STORY - 1 }, () => image);
    const first = { type: 'image' as const, uri: 'a' };
    const second = { type: 'image' as const, uri: 'b' };

    const admission = admitMedia(existing, [first, second]);

    expect(admission.accepted).toEqual([first]);
    expect(admission.mediaLimitReached).toBe(true);
  });

  it('counts the clips already in the story, not just the pick', () => {
    const admission = admitMedia([image, video, video], [video]);

    expect(admission.accepted).toEqual([video]);
    expect(admission.videoLimitReached).toBe(false);

    const refused = admitMedia([image, video, video, video], [video]);
    expect(refused.accepted).toEqual([]);
    expect(refused.videoLimitReached).toBe(true);
  });
});

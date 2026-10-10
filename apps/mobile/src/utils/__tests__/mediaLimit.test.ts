import {
  MAX_MEDIA_PER_STORY,
  MEDIA_LIMIT_MESSAGE,
  canAddMedia,
  mediaCountLabel,
  remainingMediaSlots,
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

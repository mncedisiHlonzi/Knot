import {
  clampViewerIndex,
  scrubSeconds,
  viewerIndexForId,
  viewerProgress,
  viewerSnippet,
  viewerTimeLabel,
} from '../mediaViewer';

describe('clampViewerIndex', () => {
  it('keeps an index inside the list', () => {
    expect(clampViewerIndex(0, 3)).toBe(0);
    expect(clampViewerIndex(2, 3)).toBe(2);
    expect(clampViewerIndex(9, 3)).toBe(2);
    expect(clampViewerIndex(-4, 3)).toBe(0);
  });

  it('rounds a fractional index and resolves an unusable one to the first page', () => {
    expect(clampViewerIndex(1.6, 3)).toBe(2);
    expect(clampViewerIndex(Number.NaN, 3)).toBe(0);
    expect(clampViewerIndex(Number.POSITIVE_INFINITY, 3)).toBe(0);
  });

  it('returns the first page when there is nothing to show', () => {
    expect(clampViewerIndex(2, 0)).toBe(0);
    expect(clampViewerIndex(2, -1)).toBe(0);
  });
});

describe('viewerProgress', () => {
  it('reports the played fraction', () => {
    expect(viewerProgress(0, 100)).toBe(0);
    expect(viewerProgress(25, 100)).toBe(0.25);
    expect(viewerProgress(100, 100)).toBe(1);
  });

  it('clamps to the track and rests at zero without a duration', () => {
    expect(viewerProgress(150, 100)).toBe(1);
    expect(viewerProgress(-5, 100)).toBe(0);
    expect(viewerProgress(20, 0)).toBe(0);
    expect(viewerProgress(Number.NaN, 100)).toBe(0);
  });
});

describe('scrubSeconds', () => {
  it('turns a fraction of the track into a time', () => {
    expect(scrubSeconds(0, 100)).toBe(0);
    expect(scrubSeconds(0.25, 100)).toBe(25);
    expect(scrubSeconds(1, 100)).toBe(100);
  });

  it('parks at either end when dragged past the track', () => {
    expect(scrubSeconds(-0.5, 100)).toBe(0);
    expect(scrubSeconds(2, 100)).toBe(100);
  });

  it('returns zero when there is no duration to seek within', () => {
    expect(scrubSeconds(0.5, 0)).toBe(0);
    expect(scrubSeconds(0.5, Number.NaN)).toBe(0);
  });
});

describe('viewerSnippet', () => {
  it('returns a short body whole', () => {
    expect(viewerSnippet('A short story.')).toEqual({ text: 'A short story.', truncated: false });
  });

  it('collapses whitespace before quoting', () => {
    expect(viewerSnippet('  First line.\n\nSecond   line.  ')).toEqual({
      text: 'First line. Second line.',
      truncated: false,
    });
  });

  it('cuts a long body at a word boundary and says it is an excerpt', () => {
    const excerpt = viewerSnippet('word '.repeat(80), 20);
    expect(excerpt.truncated).toBe(true);
    expect(excerpt.text.endsWith('…')).toBe(true);
    expect(excerpt.text.length).toBeLessThanOrEqual(21);
    expect(excerpt.text).not.toContain('wor…');
  });

  it('treats a blank body as nothing to quote', () => {
    expect(viewerSnippet('')).toEqual({ text: '', truncated: false });
    expect(viewerSnippet('   \n  ')).toEqual({ text: '', truncated: false });
  });

  it('never truncates when there is no window to cut to', () => {
    expect(viewerSnippet('Some body text.', 0)).toEqual({
      text: 'Some body text.',
      truncated: false,
    });
  });
});

describe('viewerTimeLabel', () => {
  it('reads a running clock from the start', () => {
    expect(viewerTimeLabel(0)).toBe('0:00');
    expect(viewerTimeLabel(12)).toBe('0:12');
    expect(viewerTimeLabel(45)).toBe('0:45');
  });

  it('keeps the start of the clip readable when the duration is unknown', () => {
    expect(viewerTimeLabel(Number.NaN)).toBe('0:00');
    expect(viewerTimeLabel(-3)).toBe('0:00');
  });

  it('reads past an hour like the duration badge does', () => {
    expect(viewerTimeLabel(3725)).toBe('1:02:05');
  });
});

describe('viewerIndexForId', () => {
  it('resolves a reported key to its page', () => {
    expect(viewerIndexForId(['a', 'b', 'c'], 'a')).toBe(0);
    expect(viewerIndexForId(['a', 'b', 'c'], 'c')).toBe(2);
  });

  it('reports a key this viewer does not hold instead of guessing a page', () => {
    expect(viewerIndexForId(['a', 'b'], 'gone')).toBe(-1);
    expect(viewerIndexForId([], 'a')).toBe(-1);
    expect(viewerIndexForId(['a'], '')).toBe(-1);
  });
});

import { clampViewerIndex, scrubSeconds, viewerProgress } from '../mediaViewer';

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

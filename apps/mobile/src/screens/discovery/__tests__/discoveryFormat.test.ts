import {
  MARKER_MAX_SIZE,
  MARKER_MIN_SIZE,
  markerCountLabel,
  markerSize,
  placeSummaryLabel,
  storyCountLabel,
} from '../discoveryFormat';

describe('storyCountLabel', () => {
  it('singularises one story', () => {
    expect(storyCountLabel(1)).toBe('1 story');
  });

  it('pluralises every other count', () => {
    expect(storyCountLabel(0)).toBe('0 stories');
    expect(storyCountLabel(24)).toBe('24 stories');
  });
});

describe('placeSummaryLabel', () => {
  it('reads the story count with the pillar break-down, Heritage first', () => {
    expect(placeSummaryLabel(24, { wonder: 10, heritage: 14 })).toBe(
      '24 stories · 14 Heritage · 10 Wonder',
    );
  });

  it('omits a pillar with no stories', () => {
    expect(placeSummaryLabel(5, { wonder: 5, heritage: 0 })).toBe('5 stories · 5 Wonder');
  });

  it('is the story count alone when no pillar is counted', () => {
    expect(placeSummaryLabel(1, {})).toBe('1 story');
    expect(placeSummaryLabel(1, { other: 3 })).toBe('1 story');
  });
});

describe('markerSize', () => {
  it('gives the busiest cluster the largest marker', () => {
    expect(markerSize(24, 24)).toBe(MARKER_MAX_SIZE);
  });

  it('scales a smaller cluster between the bounds', () => {
    const size = markerSize(12, 24);
    expect(size).toBeGreaterThan(MARKER_MIN_SIZE);
    expect(size).toBeLessThan(MARKER_MAX_SIZE);
  });

  it('clamps a count above the maximum', () => {
    expect(markerSize(30, 24)).toBe(MARKER_MAX_SIZE);
  });

  it('falls back to the smallest marker when there is no maximum', () => {
    expect(markerSize(3, 0)).toBe(MARKER_MIN_SIZE);
    expect(markerSize(0, 0)).toBe(MARKER_MIN_SIZE);
    expect(markerSize(Number.NaN, 10)).toBe(MARKER_MIN_SIZE);
  });
});

describe('markerCountLabel', () => {
  it('prints the count', () => {
    expect(markerCountLabel(24)).toBe('24');
  });

  it('keeps 99 and caps beyond it', () => {
    expect(markerCountLabel(99)).toBe('99');
    expect(markerCountLabel(120)).toBe('99+');
  });
});

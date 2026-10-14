import {
  MARKER_MAX_SIZE,
  MARKER_MIN_SIZE,
  markerCountLabel,
  markerSize,
  placeCountLabel,
  placeMapPoint,
  placeSummaryLabel,
  placeTotalsLabel,
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

describe('placeCountLabel', () => {
  it('singularises one place', () => {
    expect(placeCountLabel(1)).toBe('1 place');
  });

  it('pluralises every other count', () => {
    expect(placeCountLabel(0)).toBe('0 places');
    expect(placeCountLabel(5)).toBe('5 places');
  });
});

describe('placeTotalsLabel', () => {
  it('reads places, stories, then the pillar break-down, Heritage first', () => {
    expect(placeTotalsLabel(1, 24, { wonder: 10, heritage: 14 })).toBe(
      '1 place · 24 stories · 14 Heritage · 10 Wonder',
    );
  });

  it('omits a pillar with no stories', () => {
    expect(placeTotalsLabel(3, 12, { wonder: 8, heritage: 4 })).toBe(
      '3 places · 12 stories · 4 Heritage · 8 Wonder',
    );
  });

  it('is places and stories alone when no pillar is counted', () => {
    expect(placeTotalsLabel(2, 2, {})).toBe('2 places · 2 stories');
  });
});

describe('placeMapPoint', () => {
  it('returns the point when both numbers are present', () => {
    expect(placeMapPoint(39.9042, 116.4074)).toEqual({ latitude: 39.9042, longitude: 116.4074 });
  });

  it('accepts the extreme of each axis', () => {
    expect(placeMapPoint(90, 180)).toEqual({ latitude: 90, longitude: 180 });
    expect(placeMapPoint(-90, -180)).toEqual({ latitude: -90, longitude: -180 });
  });

  it('returns undefined when either coordinate is missing', () => {
    // A place told only by name has no coordinate, so the strip is skipped.
    expect(placeMapPoint(null, 116.4074)).toBeUndefined();
    expect(placeMapPoint(39.9042, null)).toBeUndefined();
    expect(placeMapPoint(undefined, undefined)).toBeUndefined();
  });

  it('rejects non-finite and out-of-range values', () => {
    // A marker at NaN or outside the globe would break the native map, so these
    // mean "no strip" rather than a point at 0,0.
    expect(placeMapPoint(Number.NaN, 116.4074)).toBeUndefined();
    expect(placeMapPoint(39.9042, Number.POSITIVE_INFINITY)).toBeUndefined();
    expect(placeMapPoint(90.1, 0)).toBeUndefined();
    expect(placeMapPoint(0, 180.1)).toBeUndefined();
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

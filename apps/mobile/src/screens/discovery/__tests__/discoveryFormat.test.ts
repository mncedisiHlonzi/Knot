import {
  PILLAR_TABS,
  SHEET_SNAP_RATIOS,
  adjacentSheetRatio,
  clampSheetRatio,
  filterPlacesByQuery,
  mapTopInset,
  nearestSheetRatio,
  pillarEmptyHeadline,
  placeCountLabel,
  placeMapPoint,
  placeSummaryLabel,
  placeTotalsLabel,
  sheetRatioAfterDrag,
  sheetRatioDuringDrag,
  sheetSnapHeight,
  storyCountLabel,
} from '../discoveryFormat';

describe('PILLAR_TABS', () => {
  it('offers All, Heritage, Wonder in the reference order', () => {
    expect(PILLAR_TABS.map((tab) => tab.label)).toEqual(['All', 'Wonder', 'Heritage']);
    expect(PILLAR_TABS.map((tab) => tab.value)).toEqual(['all', 'wonder', 'heritage']);
  });

  it('starts on the unfiltered tab', () => {
    expect(PILLAR_TABS[0].value).toBe('all');
  });
});

describe('mapTopInset', () => {
  it('clears the status bar on Android, which lays the app out from the top edge', () => {
    expect(mapTopInset('android', 24)).toBe(24);
    expect(mapTopInset('android', 44)).toBe(44);
  });

  it('adds nothing on iOS, whose root safe area has already inset the screen', () => {
    expect(mapTopInset('ios', undefined)).toBe(0);
    expect(mapTopInset('ios', 59)).toBe(0);
  });

  it('reads a missing or nonsensical height as no inset rather than pushing a band off screen', () => {
    expect(mapTopInset('android', undefined)).toBe(0);
    expect(mapTopInset('android', 0)).toBe(0);
    expect(mapTopInset('android', -24)).toBe(0);
  });

  it('is inert on a platform it does not know', () => {
    expect(mapTopInset('web', 24)).toBe(0);
  });
});

describe('pillarEmptyHeadline', () => {
  it('names the lens when a filtered tab is empty', () => {
    expect(pillarEmptyHeadline('wonder')).toBe('No Wonder stories here yet.');
    expect(pillarEmptyHeadline('heritage')).toBe('No Heritage stories here yet.');
  });

  it('is unqualified for the All tab', () => {
    expect(pillarEmptyHeadline('all')).toBe('No stories here yet.');
  });
});

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

describe('filterPlacesByQuery', () => {
  const places = [
    { place: 'Oaxaca', place_country: 'Mexico' },
    { place: 'Manguzi', place_country: 'South Africa' },
    { place: 'Cape Town', place_country: 'South Africa' },
  ];

  it('returns every place for a blank query, so clearing the field restores the list', () => {
    expect(filterPlacesByQuery(places, '')).toEqual(places);
    expect(filterPlacesByQuery(places, '   ')).toEqual(places);
  });

  it('matches a place name case-insensitively, anywhere in the name', () => {
    expect(filterPlacesByQuery(places, 'oax')).toEqual([places[0]]);
    expect(filterPlacesByQuery(places, 'OAXACA')).toEqual([places[0]]);
    expect(filterPlacesByQuery(places, ' town ')).toEqual([places[2]]);
  });

  it('matches the name only, not the country shown beside it', () => {
    expect(filterPlacesByQuery(places, 'South Africa')).toEqual([]);
  });

  it('returns nothing when no place matches, so the sheet can say so', () => {
    expect(filterPlacesByQuery(places, 'Atlantis')).toEqual([]);
  });
});

describe('the places sheet snap points', () => {
  it('offers three ascending points: collapsed, half and expanded', () => {
    expect(SHEET_SNAP_RATIOS).toEqual([0.5, 0.75, 0.95]);
  });

  it('converts a ratio to a whole-point height', () => {
    expect(sheetSnapHeight(0.5, 800)).toBe(400);
    expect(sheetSnapHeight(0.75, 800)).toBe(600);
    expect(sheetSnapHeight(0.95, 800)).toBe(760);
    // A layout that has not been measured yet has no height to give.
    expect(sheetSnapHeight(0.5, 0)).toBe(0);
    expect(sheetSnapHeight(Number.NaN, 800)).toBe(0);
  });

  it('holds a dragged ratio inside the sheet travel', () => {
    expect(clampSheetRatio(0.1)).toBe(0.5);
    expect(clampSheetRatio(2)).toBe(0.95);
    expect(clampSheetRatio(0.62)).toBe(0.62);
    expect(clampSheetRatio(Number.NaN)).toBe(0.5);
  });

  it('snaps to the nearest point, keeping the lower point on a tie', () => {
    expect(nearestSheetRatio(0.51)).toBe(0.5);
    // 0.625 is exactly between collapsed and half; the sheet springs back.
    expect(nearestSheetRatio(0.625)).toBe(0.5);
    expect(nearestSheetRatio(0.63)).toBe(0.75);
    expect(nearestSheetRatio(0.9)).toBe(0.95);
    expect(nearestSheetRatio(3)).toBe(0.95);
  });

  it('follows the finger while dragging, then settles on the nearest point', () => {
    // A quarter-screen drag up from collapsed lands exactly on half.
    expect(sheetRatioDuringDrag(0.5, -200, 800)).toBe(0.75);
    expect(sheetRatioAfterDrag(0.5, -200, 800)).toBe(0.75);
    // A short drag that passes no midpoint springs back.
    expect(sheetRatioAfterDrag(0.5, -40, 800)).toBe(0.5);
    // A down drag from expanded settles on half.
    expect(sheetRatioAfterDrag(0.95, 160, 800)).toBe(0.75);
    // Dragging past the top pins at expanded rather than leaving the screen.
    expect(sheetRatioDuringDrag(0.5, -1000, 800)).toBe(0.95);
    expect(sheetRatioAfterDrag(0.5, -1000, 800)).toBe(0.95);
  });

  it('steps to an adjacent point and stays put at either end', () => {
    expect(adjacentSheetRatio(0.5, 1)).toBe(0.75);
    expect(adjacentSheetRatio(0.75, 1)).toBe(0.95);
    expect(adjacentSheetRatio(0.95, 1)).toBe(0.95);
    expect(adjacentSheetRatio(0.5, -1)).toBe(0.5);
    // A ratio between points starts from the nearest one.
    expect(adjacentSheetRatio(0.9, -1)).toBe(0.75);
  });
});

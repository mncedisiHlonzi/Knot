import {
  INLINE_VIEWABILITY_CONFIG,
  VIEWABILITY_CONFIG,
  activeViewableIndex,
  activeViewableKey,
  isActiveEntry,
  nextKeyAfter,
} from '../viewability';

/**
 * The viewability decision is pure, so it is exercised directly — no renderer,
 * which is the project's test pattern (there is no React Native Testing Library
 * and this task must not add one).
 */
describe('viewability', () => {
  it('activates an item only once it is at least 60% visible', () => {
    expect(VIEWABILITY_CONFIG.itemVisiblePercentThreshold).toBe(60);
  });

  it('takes the first viewable item as the active one, as a string key', () => {
    expect(activeViewableKey({ viewableItems: [{ key: 'story-2' }] })).toBe('story-2');
    expect(activeViewableKey({ viewableItems: [{ key: 7 }, { key: 9 }] })).toBe('7');
  });

  it('has no active item when nothing is viewable', () => {
    expect(activeViewableKey({ viewableItems: [] })).toBeUndefined();
  });
});

describe('nextKeyAfter', () => {
  const keys = ['a', 'b', 'c'];

  it('returns the item after the active one', () => {
    expect(nextKeyAfter(keys, 'a')).toBe('b');
    expect(nextKeyAfter(keys, 'b')).toBe('c');
  });

  it('has nothing to preload at the end, or when nothing is active', () => {
    expect(nextKeyAfter(keys, 'c')).toBeUndefined();
    expect(nextKeyAfter(keys, undefined)).toBeUndefined();
  });

  it('has nothing to preload when the active item has left the list', () => {
    expect(nextKeyAfter(keys, 'gone')).toBeUndefined();
  });
});

/**
 * A list that plays a clip inside its own rows measures its active card as a share
 * of the VIEWPORT, not as a share of the card: a feed card is about as tall as the
 * screen, so "90% of the card visible" would exclude any card taller than the
 * viewport and its video would never play (KNOT-050).
 */
describe('the inline player rule', () => {
  it('counts the card that owns most of the viewport', () => {
    // "Most", not "almost all": two cards share a feed screen most of the time, and
    // a strict 90% would leave every video a still.
    expect(INLINE_VIEWABILITY_CONFIG.viewAreaCoveragePercentThreshold).toBe(50);
  });

  it('uses exactly one threshold, which the list itself requires', () => {
    const config: Record<string, unknown> = INLINE_VIEWABILITY_CONFIG;

    expect(config.itemVisiblePercentThreshold).toBeUndefined();
  });

  it('reports the active card by index, so a row needs no lookup', () => {
    expect(activeViewableIndex({ viewableItems: [{ key: 'story-2', index: 4 }] })).toBe(4);
    // The first viewable item is the one on screen; the rest are neighbours.
    expect(
      activeViewableIndex({
        viewableItems: [
          { key: 'story-2', index: 4 },
          { key: 'story-3', index: 5 },
        ],
      }),
    ).toBe(4);
  });

  it('has no active card when nothing is viewable, or no index was reported', () => {
    expect(activeViewableIndex({ viewableItems: [] })).toBeUndefined();
    expect(activeViewableIndex({ viewableItems: [{ key: 'story-2' }] })).toBeUndefined();
    expect(
      activeViewableIndex({ viewableItems: [{ key: 'story-2', index: null }] }),
    ).toBeUndefined();
  });
});

describe('isActiveEntry', () => {
  it('gives the player to the active card alone', () => {
    expect(isActiveEntry(2, 2)).toBe(true);
    expect(isActiveEntry(2, 1)).toBe(false);
    expect(isActiveEntry(2, 3)).toBe(false);
  });

  it('never reads a missing index as row zero', () => {
    // `undefined === undefined` is true, which would mount a player on every row.
    expect(isActiveEntry(undefined, 0)).toBe(false);
  });
});

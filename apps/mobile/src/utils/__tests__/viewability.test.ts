import { VIEWABILITY_CONFIG, activeViewableKey, nextKeyAfter } from '../viewability';

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

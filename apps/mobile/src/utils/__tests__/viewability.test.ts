import { VIEWABILITY_CONFIG, activeViewableKey } from '../viewability';

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

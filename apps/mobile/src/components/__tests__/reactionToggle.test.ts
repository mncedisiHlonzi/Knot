import { applyReactionToggle, asReactionTypes, holdsReaction } from '../reactionToggle';

/**
 * ReactionBar is a stateful component and there is no renderer in this project, so
 * its optimistic-toggle logic lives in `reactionToggle.ts` and is tested here
 * directly (KNOT-020b).
 */
describe('asReactionTypes', () => {
  it('keeps only the signals this build knows', () => {
    expect(asReactionTypes(['rings_true', 'nonsense', 'needs_a_source'])).toEqual([
      'rings_true',
      'needs_a_source',
    ]);
  });

  it('drops a non-array-safe value without throwing', () => {
    expect(() => asReactionTypes([])).not.toThrow();
  });
});

describe('holdsReaction', () => {
  it('reports whether a signal is held', () => {
    expect(holdsReaction(['rings_true'], 'rings_true')).toBe(true);
    expect(holdsReaction(['rings_true'], 'needs_a_source')).toBe(false);
  });
});

describe('applyReactionToggle', () => {
  const counts = {
    rings_true: 2,
    know_it_differently: 0,
    adds_something_new: 0,
    needs_a_source: 0,
  };

  it('adds a signal the reader does not hold', () => {
    const result = applyReactionToggle([], counts, 'rings_true');
    expect(result.counts.rings_true).toBe(3);
    expect(result.myReactions).toEqual(['rings_true']);
  });

  it('removes a signal the reader already holds', () => {
    const result = applyReactionToggle(['rings_true'], counts, 'rings_true');
    expect(result.counts.rings_true).toBe(1);
    expect(result.myReactions).toEqual([]);
  });

  it('never drops a count below zero', () => {
    const result = applyReactionToggle(['rings_true'], { ...counts, rings_true: 0 }, 'rings_true');
    expect(result.counts.rings_true).toBe(0);
  });

  it('leaves the other signals untouched', () => {
    const result = applyReactionToggle([], counts, 'adds_something_new');
    expect(result.counts.rings_true).toBe(2);
    expect(result.counts.adds_something_new).toBe(1);
  });
});

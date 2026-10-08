import {
  OverlayScreen,
  popAllOverlays,
  popOverlay,
  pushOverlay,
  replaceOverlay,
  topOverlay,
} from '../overlayStack';

const DETAIL: OverlayScreen = { name: 'detail', props: { storyId: 'story-1' } };
const TREE: OverlayScreen = { name: 'tree', props: { storyId: 'story-1' } };
const ADAPT: OverlayScreen = { name: 'adapt', props: { storyId: 'story-1' } };

describe('overlayStack', () => {
  it('pushes onto the top of the stack', () => {
    expect(pushOverlay([DETAIL], TREE)).toEqual([DETAIL, TREE]);
  });

  it('pops the top of the stack', () => {
    expect(popOverlay([DETAIL, TREE])).toEqual([DETAIL]);
  });

  it('popping an empty stack is a no-op', () => {
    expect(popOverlay([])).toEqual([]);
  });

  it('replaces the top of the stack without changing its depth', () => {
    expect(replaceOverlay([DETAIL, TREE], ADAPT)).toEqual([DETAIL, ADAPT]);
  });

  it('replacing on an empty stack pushes', () => {
    expect(replaceOverlay<OverlayScreen>([], DETAIL)).toEqual([DETAIL]);
  });

  it('empties the stack', () => {
    expect(popAllOverlays([DETAIL, TREE])).toEqual([]);
  });

  it('returns the top overlay, or undefined when the stack is empty', () => {
    expect(topOverlay([DETAIL, TREE])).toBe(TREE);
    expect(topOverlay([])).toBeUndefined();
  });

  it('does not mutate the input array', () => {
    const stack: readonly OverlayScreen[] = [DETAIL];

    pushOverlay(stack, TREE);
    popOverlay(stack);
    replaceOverlay(stack, ADAPT);
    popAllOverlays(stack);

    expect(stack).toEqual([DETAIL]);
  });
});

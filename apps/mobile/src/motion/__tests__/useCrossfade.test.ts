import { useCrossfade } from '../useCrossfade';

/**
 * `useCrossfade` watches the mode flip, paints the canvas being LEFT and fades it
 * away. The suite scripts React's primitives and asserts the colour it chooses and
 * the fade it asks for — the observable contract of the effect (KNOT-020k).
 */
let mockStates: unknown[] = [];
let mockStateIndex = 0;
let mockRefs: Array<{ current: unknown }> = [];
let mockRefIndex = 0;
let mockEffects: Array<() => void | (() => void)> = [];

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useState: (initial: unknown) => {
    const index = mockStateIndex;
    mockStateIndex += 1;
    if (!(index in mockStates)) {
      mockStates[index] = typeof initial === 'function' ? (initial as () => unknown)() : initial;
    }
    return [
      mockStates[index],
      (next: unknown) => {
        mockStates[index] = next;
      },
    ];
  },
  useRef: (initial: unknown) => {
    const index = mockRefIndex;
    mockRefIndex += 1;
    if (mockRefs[index] === undefined) {
      mockRefs[index] = { current: initial };
    }
    return mockRefs[index];
  },
  useEffect: (effect: () => void | (() => void)) => {
    mockEffects.push(effect);
  },
}));

let mockComplete: ((result: { finished: boolean }) => void) | undefined;
const mockTiming = jest.fn((..._args: unknown[]) => ({
  start: (callback?: (result: { finished: boolean }) => void) => {
    mockComplete = callback;
  },
}));

jest.mock('react-native', () => ({
  Animated: {
    Value: class {
      setValue(): void {}
    },
    timing: (...args: unknown[]) => mockTiming(...(args as [])),
  },
  Easing: { bezier: () => (progress: number) => progress },
}));

const mockReduced = { value: false };
jest.mock('../../hooks/useReducedMotion', () => ({
  useReducedMotion: () => mockReduced.value,
}));

/** One "render": hook state persists in the slots, effects run afterwards. */
function render(isDark: boolean): ReturnType<typeof useCrossfade> {
  mockStateIndex = 0;
  mockRefIndex = 0;
  mockEffects = [];
  useCrossfade(isDark);
  for (const effect of mockEffects) {
    effect();
  }
  // The state setters run inside the effect, so the hook is read again for the
  // value the component would render next.
  mockStateIndex = 0;
  mockRefIndex = 0;
  mockEffects = [];
  return useCrossfade(isDark);
}

describe('useCrossfade', () => {
  beforeEach(() => {
    mockStates = [];
    mockRefs = [];
    mockReduced.value = false;
    mockComplete = undefined;
    mockTiming.mockClear();
  });

  it('draws nothing on the first render', () => {
    expect(render(false).color).toBeNull();
    expect(mockTiming).not.toHaveBeenCalled();
  });

  it('paints the canvas being left and fades it out over the medium duration', () => {
    render(false);
    const crossfade = render(true);

    // Leaving light mode: the overlay is the light canvas, going to transparent.
    expect(crossfade.color).toBe('#FFFFFF');
    expect(mockTiming.mock.calls[0]?.[1]).toMatchObject({
      toValue: 0,
      duration: 250,
      useNativeDriver: true,
    });
  });

  it('paints the dark canvas when leaving dark mode', () => {
    render(true);
    const crossfade = render(false);

    expect(crossfade.color).toBe('#000000');
  });

  it('removes the overlay when the fade completes', () => {
    render(false);
    render(true);

    mockComplete?.({ finished: true });

    // The completion callback cleared the colour, so the next render draws nothing.
    expect(render(true).color).toBeNull();
  });

  it('snaps with no duration when motion is reduced', () => {
    mockReduced.value = true;
    render(false);
    render(true);

    expect(mockTiming.mock.calls[0]?.[1]).toMatchObject({ duration: 0 });
  });
});

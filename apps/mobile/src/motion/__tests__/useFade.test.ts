import { useFade } from '../useFade';

/**
 * `useFade` drives one opacity value in and out and reports completion, so the
 * caller can unmount a surface after its exit animation. The suite scripts React's
 * primitives and records what the hook asks `Animated` for (KNOT-020k).
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
  useCallback: (callback: unknown) => callback,
  useEffect: (effect: () => void | (() => void)) => {
    mockEffects.push(effect);
  },
}));

let mockTimingResult: { finished: boolean } = { finished: true };
const mockTiming = jest.fn((..._args: unknown[]) => ({
  start: (callback?: (result: { finished: boolean }) => void) => {
    callback?.(mockTimingResult);
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

/** One "render": the hook re-runs against the state slots it already has. */
function render(initialVisible = false): ReturnType<typeof useFade> {
  mockStateIndex = 0;
  mockRefIndex = 0;
  mockEffects = [];
  return useFade(initialVisible);
}

describe('useFade', () => {
  beforeEach(() => {
    mockStates = [];
    mockRefs = [];
    mockReduced.value = false;
    mockTimingResult = { finished: true };
    mockTiming.mockClear();
  });

  it('fades in over the short duration', () => {
    const fade = render();

    fade.fadeIn();

    expect(mockTiming.mock.calls[0]?.[1]).toMatchObject({
      toValue: 1,
      duration: 150,
      useNativeDriver: true,
    });
  });

  it('fades out and reports completion so the caller can unmount', () => {
    const fade = render();
    const done = jest.fn();

    fade.fadeOut(done);

    expect(mockTiming.mock.calls[0]?.[1]).toMatchObject({ toValue: 0, duration: 150 });
    expect(done).toHaveBeenCalledTimes(1);
  });

  it('does not report completion for an interrupted exit', () => {
    const fade = render();
    const done = jest.fn();
    mockTimingResult = { finished: false };

    fade.fadeOut(done);

    expect(done).not.toHaveBeenCalled();
  });

  it('fades in on mount when it starts visible', () => {
    render(true);

    expect(mockEffects).toHaveLength(1);
    mockEffects[0]?.();
    expect(mockTiming.mock.calls[0]?.[1]).toMatchObject({ toValue: 1 });
  });

  it('does nothing on mount when it starts hidden', () => {
    render(false);

    mockEffects[0]?.();
    expect(mockTiming).not.toHaveBeenCalled();
  });

  it('snaps both directions with no duration when motion is reduced', () => {
    mockReduced.value = true;
    const fade = render();

    fade.fadeIn();
    fade.fadeOut();

    expect(mockTiming.mock.calls[0]?.[1]).toMatchObject({ duration: 0 });
    expect(mockTiming.mock.calls[1]?.[1]).toMatchObject({ duration: 0 });
  });
});

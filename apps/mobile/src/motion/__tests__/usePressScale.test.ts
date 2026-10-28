import { usePressScale } from '../usePressScale';

/**
 * The press-scale runs one `Animated.timing` per press. This suite has no
 * renderer, so React's primitives are scripted (state in slots, callbacks called
 * straight through) and React Native's `Animated`/`Easing` are recorders — the
 * assertions are on what the primitive asks the driver for, which is the whole
 * contract: the target scale, the duration and the native driver (KNOT-020k).
 */
let mockStates: unknown[] = [];
let mockStateIndex = 0;
let mockRefs: Array<{ current: unknown }> = [];
let mockRefIndex = 0;

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
  useMemo: (factory: () => unknown) => factory(),
  useEffect: () => undefined,
}));

const mockStart = jest.fn();
const mockTiming = jest.fn((..._args: unknown[]) => ({ start: mockStart }));

jest.mock('react-native', () => ({
  Animated: {
    Value: class {
      setValue(): void {}
    },
    timing: (...args: unknown[]) => mockTiming(...(args as [])),
  },
  Easing: { bezier: () => (progress: number) => progress },
}));

// The reduce-motion preference is the primitive's input, so it is scripted here
// rather than read from the OS; its own suite covers the OS read (KNOT-020k).
const mockReduced = { value: false };
jest.mock('../../hooks/useReducedMotion', () => ({
  useReducedMotion: () => mockReduced.value,
}));

/** One "render": the hook re-runs against the state slots it already has. */
function render(enabled = true): ReturnType<typeof usePressScale> {
  mockStateIndex = 0;
  mockRefIndex = 0;
  return usePressScale(enabled);
}

describe('usePressScale', () => {
  beforeEach(() => {
    mockStates = [];
    mockRefs = [];
    mockReduced.value = false;
    mockTiming.mockClear();
    mockStart.mockClear();
  });

  it('starts at rest and shrinks to the press scale with the shared duration', () => {
    const press = render();

    press.onPressIn();

    expect(press.scale).toBeDefined();
    expect(mockTiming).toHaveBeenCalledTimes(1);
    expect(mockTiming.mock.calls[0]?.[1]).toMatchObject({
      toValue: 0.97,
      duration: 150,
      useNativeDriver: true,
    });
  });

  it('returns to rest on release', () => {
    const press = render();

    press.onPressOut();

    expect(mockTiming.mock.calls[0]?.[1]).toMatchObject({ toValue: 1, duration: 150 });
  });

  it('snaps to the final value with no duration when motion is reduced', () => {
    mockReduced.value = true;
    const press = render();

    press.onPressIn();

    expect(mockTiming.mock.calls[0]?.[1]).toMatchObject({ toValue: 0.97, duration: 0 });
  });

  it('stays put when the caller disables it (a disabled control)', () => {
    const press = render(false);

    press.onPressIn();

    expect(mockTiming.mock.calls[0]?.[1]).toMatchObject({ duration: 0 });
  });
});

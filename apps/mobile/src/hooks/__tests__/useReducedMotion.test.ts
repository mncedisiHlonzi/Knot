import { useReducedMotion } from '../useReducedMotion';

/**
 * `useReducedMotion` reads a native accessibility module and subscribes to it.
 * The suite has no renderer, so React's hook primitives are scripted: state is
 * held in a slot array (the way a hook would hold it across renders), an effect
 * is captured so it can be run explicitly, and React Native's
 * `AccessibilityInfo` is a recorder (KNOT-020k).
 */
let mockStates: unknown[] = [];
let mockStateIndex = 0;
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
  useEffect: (effect: () => void | (() => void)) => {
    mockEffects.push(effect);
  },
}));

const mockRemove = jest.fn();
let mockListener: ((enabled: boolean) => void) | undefined;

jest.mock('react-native', () => ({
  AccessibilityInfo: {
    isReduceMotionEnabled: jest.fn(async () => false),
    addEventListener: jest.fn((_event: string, handler: (enabled: boolean) => void) => {
      mockListener = handler;
      return { remove: mockRemove };
    }),
  },
}));

const AccessibilityInfo = jest.requireMock('react-native').AccessibilityInfo as {
  isReduceMotionEnabled: jest.Mock;
  addEventListener: jest.Mock;
};

/** One "render": the hook re-runs against the state slots it already has. */
function render(): boolean {
  mockStateIndex = 0;
  mockEffects = [];
  return useReducedMotion();
}

/** Runs the effects the last render registered, then lets their promises settle. */
async function runEffects(): Promise<void> {
  for (const effect of mockEffects) {
    effect();
  }
  await Promise.resolve();
  await Promise.resolve();
}

describe('useReducedMotion', () => {
  beforeEach(() => {
    mockStates = [];
    mockStateIndex = 0;
    mockEffects = [];
    mockListener = undefined;
    mockRemove.mockClear();
    AccessibilityInfo.isReduceMotionEnabled.mockReset();
    AccessibilityInfo.isReduceMotionEnabled.mockResolvedValue(false);
    AccessibilityInfo.addEventListener.mockClear();
  });

  it('starts false and reports what the OS says', async () => {
    AccessibilityInfo.isReduceMotionEnabled.mockResolvedValue(true);

    expect(render()).toBe(false);
    await runEffects();

    expect(render()).toBe(true);
  });

  it('follows the OS while the app runs', async () => {
    expect(render()).toBe(false);
    await runEffects();

    expect(typeof mockListener).toBe('function');
    mockListener?.(true);
    expect(render()).toBe(true);

    mockListener?.(false);
    expect(render()).toBe(false);
  });

  it('subscribes once and unsubscribes on unmount', async () => {
    render();
    const cleanup = mockEffects[0]?.();
    expect(AccessibilityInfo.addEventListener).toHaveBeenCalledTimes(1);
    expect(AccessibilityInfo.addEventListener).toHaveBeenCalledWith(
      'reduceMotionChanged',
      expect.any(Function),
    );

    expect(typeof cleanup).toBe('function');
    (cleanup as () => void)();
    expect(mockRemove).toHaveBeenCalledTimes(1);
  });

  it('treats a failed read as "motion is allowed"', async () => {
    AccessibilityInfo.isReduceMotionEnabled.mockRejectedValue(new Error('unavailable'));

    expect(render()).toBe(false);
    await runEffects();

    expect(render()).toBe(false);
  });
});

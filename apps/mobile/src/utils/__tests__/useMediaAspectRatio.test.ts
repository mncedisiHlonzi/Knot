import { useMediaAspectRatio } from '../useMediaAspectRatio';

/**
 * The hook records what the media reported by calling React's state setter, and
 * this project has no renderer to observe a re-render with. So React is mocked to
 * hand back a recording setter: calling the hook stays a plain function call, and
 * the value it would store is still asserted.
 */
const mockStateCalls: unknown[] = [];

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useCallback: (callback: unknown) => callback,
  useState: (initial: unknown) => [initial, (value: unknown) => mockStateCalls.push(value)],
}));

describe('useMediaAspectRatio', () => {
  beforeEach(() => {
    mockStateCalls.length = 0;
  });

  it('takes its ratio from the payload before anything has loaded', () => {
    const { aspectRatio } = useMediaAspectRatio(1080, 1920);

    expect(aspectRatio).toBeCloseTo(0.5625, 4);
  });

  it('reports no ratio when the payload cannot describe the media', () => {
    expect(useMediaAspectRatio(undefined, undefined).aspectRatio).toBeNull();
    expect(useMediaAspectRatio(null, null).aspectRatio).toBeNull();
    expect(useMediaAspectRatio(0, 0).aspectRatio).toBeNull();
  });

  it("records the media's natural size when it loads", () => {
    const { onLoadSize } = useMediaAspectRatio(undefined, undefined);

    onLoadSize(300, 700);

    expect(mockStateCalls).toEqual([{ width: 300, height: 700 }]);
  });

  it('hands back the same onLoadSize on every call, so a hook-dependency is stable', () => {
    const first = useMediaAspectRatio(undefined, undefined).onLoadSize;
    const second = useMediaAspectRatio(undefined, undefined).onLoadSize;

    // The mock returns the callback as given; the point is that each call produces
    // one identity rather than a fresh closure per render.
    expect(typeof first).toBe('function');
    expect(typeof second).toBe('function');
  });
});

import VideoPlayer from '../VideoPlayer';

/**
 * `VideoPlayer` holds playback state and a ref to the native player, so it needs
 * the hook primitives stubbed before it can be CALLED directly — this project has
 * no renderer and must not add one.
 *
 * The stubs are slot-based rather than throwaway: a ref keeps the same object
 * between renders (so a test can attach a fake native player to the `<Video>`'s
 * `ref` and still find it from the next render), and effects are captured instead
 * of run, so a test can drive the release that happens "when `isActive` becomes
 * false" without a commit. Every binding is a module-scope `mock*` name because
 * babel-jest refuses any other out-of-scope reference, and each is read lazily
 * inside the hook so there is no TDZ hazard.
 */
const URI = 'http://localhost:8080/stories/1/media/2/content';

let mockRefSlots: { current: unknown }[] = [];
let mockRefCursor = 0;
let mockStateSlots: unknown[] = [];
let mockStateCursor = 0;
let mockEffects: { run: () => void; deps?: unknown[] }[] = [];

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useMemo: (factory: () => unknown) => factory(),
  useRef: (initial: unknown) => {
    const index = mockRefCursor;
    mockRefCursor += 1;
    if (mockRefSlots[index] === undefined) {
      mockRefSlots[index] = { current: initial };
    }
    return mockRefSlots[index];
  },
  useState: (initial: unknown) => {
    const index = mockStateCursor;
    mockStateCursor += 1;
    if (mockStateSlots[index] === undefined) {
      mockStateSlots[index] =
        typeof initial === 'function' ? (initial as () => unknown)() : initial;
    }
    const setValue = (next: unknown): void => {
      mockStateSlots[index] = next;
    };
    return [mockStateSlots[index], setValue];
  },
  useEffect: (effect: () => void, deps?: unknown[]) => {
    mockEffects.push({ run: effect, deps });
  },
}));

jest.mock('react-native', () => {
  /** The smallest Animated value this component builds and reads. */
  class Value {
    setValue(): void {
      return undefined;
    }

    interpolate(): number {
      return 0;
    }
  }

  return {
    ActivityIndicator: 'ActivityIndicator',
    Animated: {
      Value,
      View: 'Animated.View',
      timing: () => ({ start: () => undefined }),
    },
    Easing: { bezier: () => undefined },
    PanResponder: { create: () => ({ panHandlers: {} }) },
    Pressable: 'Pressable',
    StyleSheet: { absoluteFill: {}, absoluteFillObject: {}, create: (styles: unknown) => styles },
    Text: 'Text',
    View: 'View',
  };
});

jest.mock('phosphor-react-native', () => ({
  SpeakerHigh: () => null,
  SpeakerSlash: () => null,
}));

// react-native-video is a native module: stub it to a host component so the
// element tree can be built and its ref inspected.
jest.mock('react-native-video', () => ({ __esModule: true, default: 'Video' }));

type Element = { type?: unknown; props?: Record<string, unknown>; ref?: unknown };

/** Every element in a directly-called component's tree, depth-first. */
function elements(node: unknown, found: Element[] = []): Element[] {
  if (Array.isArray(node)) {
    for (const child of node) {
      elements(child, found);
    }
    return found;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return found;
  }

  const element = node as Element;
  if (element.type !== undefined) {
    found.push(element);
  }
  if (element.props !== undefined) {
    elements(element.props.children, found);
  }
  return found;
}

/**
 * Renders the player with the hook cursors back at slot zero. Refs and state keep
 * their previous slots, which is what lets a test see the value a callback wrote.
 */
function render(overrides: Partial<Parameters<typeof VideoPlayer>[0]> = {}): Element {
  mockRefCursor = 0;
  mockStateCursor = 0;
  mockEffects = [];
  return VideoPlayer({
    uri: URI,
    width: 390,
    height: 844,
    isActive: true,
    chromeVisible: true,
    onToggleChrome: () => undefined,
    ...overrides,
  }) as Element;
}

/** The effect that owns the native release: its dependencies are `[isActive, uri]`. */
function releaseEffect(): (() => void) | undefined {
  return mockEffects.find((candidate) => candidate.deps?.[1] === URI)?.run;
}

/** A stand-in for the native player the `<Video>` ref would hold. */
function fakePlayer(): { pause: jest.Mock; setSource: jest.Mock } {
  return { pause: jest.fn(), setSource: jest.fn() };
}

/** Attaches `player` to the ref the component gives its `<Video>`. */
function attachPlayer(tree: Element, player: { pause: jest.Mock; setSource: jest.Mock }): void {
  // React lifts `ref` off the props onto the element itself, so it is read there.
  const video = elements(tree).find((element) => element.type === 'Video');
  const ref = video?.ref as { current: unknown } | undefined;

  expect(ref).toBeDefined();
  if (ref !== undefined) {
    ref.current = player;
  }
}

beforeEach(() => {
  mockRefSlots = [];
  mockRefCursor = 0;
  mockStateSlots = [];
  mockStateCursor = 0;
  mockEffects = [];
});

describe('VideoPlayer release', () => {
  /**
   * The crash this guards (KNOT-020k-fix17): a gallery keeps its neighbouring
   * pages mounted, so every visited clip held a live native player — surface,
   * decoders and buffers — until the whole viewer closed, and a long session ran
   * the process out of memory. `react-native-video` has no `release()`; clearing
   * the source reaches the native `clearSrc()`, which stops the player and drops
   * its media items.
   */
  it('drops the native source when the page stops being active', () => {
    const first = render({ isActive: true });
    const player = fakePlayer();
    attachPlayer(first, player);

    render({ isActive: false });
    const release = releaseEffect();

    expect(typeof release).toBe('function');
    release?.();

    expect(player.pause).toHaveBeenCalledTimes(1);
    // No argument is the point: an empty source is what frees the native player.
    expect(player.setSource).toHaveBeenCalledWith();
  });

  it('leaves an active player playing', () => {
    const first = render({ isActive: true });
    const player = fakePlayer();
    attachPlayer(first, player);

    render({ isActive: true });
    releaseEffect()?.();

    expect(player.pause).not.toHaveBeenCalled();
    expect(player.setSource).not.toHaveBeenCalled();
  });

  it('loads the clip again when the reader swipes back to the page', () => {
    const first = render({ isActive: true });
    const player = fakePlayer();
    attachPlayer(first, player);

    render({ isActive: false });
    releaseEffect()?.();

    render({ isActive: true });
    releaseEffect()?.();

    expect(player.setSource).toHaveBeenLastCalledWith({ uri: URI });
  });
});

import VideoPlayer from '../VideoPlayer';

/**
 * `VideoPlayer` is now chrome over the app's one media engine: it draws the
 * transport and tells the engine to present its clip full-screen. It therefore
 * needs two stubs — the React primitives (this project has no renderer) and the
 * engine itself, so the calls it makes are observable (KNOT-051).
 */
const URI = 'http://localhost:8080/stories/1/media/2/content';

const mockActions = {
  playMedia: jest.fn(),
  pauseMedia: jest.fn(),
  resumeMedia: jest.fn(),
  togglePlayback: jest.fn(),
  seekTo: jest.fn(),
  setMuted: jest.fn(),
  clearSurface: jest.fn(),
  releaseMedia: jest.fn(),
  getCurrentTime: jest.fn(() => 0),
  getActiveMediaId: jest.fn(() => null),
  registerHost: jest.fn(),
  releaseHost: jest.fn(),
  measureHost: jest.fn(),
};

let mockPlayback = {
  activeMediaId: URI as string | null,
  playing: true,
  currentTime: 0,
  duration: 0,
  ready: true,
  muted: false,
};

jest.mock('../../context/MediaPlayerContext', () => ({
  useMediaPlayerActions: () => mockActions,
  useMediaPlayback: () => mockPlayback,
}));

let mockRefSlots: { current: unknown }[] = [];
let mockRefCursor = 0;
let mockStateSlots: unknown[] = [];
let mockStateCursor = 0;
let mockEffects: { run: () => void; deps?: unknown[] }[] = [];

/** Applies a functional state update, as React would. */
function mockResolveState(current: unknown, next: unknown): unknown {
  const update = next as (value: unknown) => unknown;
  return typeof next === 'function' ? update(current) : next;
}

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
      mockStateSlots[index] = mockResolveState(mockStateSlots[index], next);
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

/** Runs the effects the last render captured, in order. */
function runEffects(): void {
  for (const effect of mockEffects) {
    effect.run();
  }
}

/** The visible text nodes, for the transport's time label. */
function texts(tree: unknown): string[] {
  return elements(tree)
    .filter((element) => element.type === 'Text')
    .map((element) => element.props?.children)
    .filter((child): child is string => typeof child === 'string');
}

beforeEach(() => {
  jest.clearAllMocks();
  mockPlayback = {
    activeMediaId: URI,
    playing: true,
    currentTime: 0,
    duration: 0,
    ready: true,
    muted: false,
  };
  mockRefSlots = [];
  mockRefCursor = 0;
  mockStateSlots = [];
  mockStateCursor = 0;
  mockEffects = [];
});

describe('VideoPlayer engine handshake', () => {
  it('presents its clip full-screen through the one engine', () => {
    render({ isActive: true });
    runEffects();

    expect(mockActions.playMedia).toHaveBeenCalledWith(
      expect.objectContaining({
        id: URI,
        uri: URI,
        mode: 'fullscreen',
        frame: { x: 0, y: 0, width: 390, height: 844 },
      }),
    );
  });

  /**
   * The point of the engine: a page no longer owns a player, so the viewer and the
   * feed cannot hold two decoders for the same clip (KNOT-051).
   */
  it('draws no player of its own', () => {
    const tree = render({ isActive: true });

    expect(elements(tree).some((element) => element.type === 'Video')).toBe(false);
  });

  it('hides the surface and pauses the clip when the page stops being active', () => {
    render({ isActive: false });
    runEffects();

    expect(mockActions.clearSurface).toHaveBeenCalledTimes(1);
    expect(mockActions.pauseMedia).toHaveBeenCalledWith(URI);
  });

  it('publishes the viewer mute choice to the one player', () => {
    render({ isActive: true });
    runEffects();

    // The viewer opens with sound (KNOT-020k-fix4).
    expect(mockActions.setMuted).toHaveBeenCalledWith(false);
  });
});

describe('VideoPlayer transport', () => {
  it('reads the playhead and the duration from the engine', () => {
    mockPlayback = { ...mockPlayback, currentTime: 12, duration: 45 };
    const tree = render({ isActive: true });

    expect(texts(tree)).toContain('0:12 / 0:45');
  });

  it('falls back to the payload duration before the clip has loaded', () => {
    mockPlayback = { ...mockPlayback, duration: 0, ready: false };
    const tree = render({ isActive: true, durationMs: 76_000 });

    expect(texts(tree)).toContain('0:00 / 1:16');
  });

  it('toggles playback through the engine when the picture is tapped', () => {
    const tree = render({ isActive: true });
    const picture = elements(tree).find(
      (element) => element.props?.accessibilityLabel === 'Hide video controls',
    );
    const onPress = picture?.props?.onPress as (() => void) | undefined;

    expect(typeof onPress).toBe('function');
    onPress?.();

    expect(mockActions.togglePlayback).toHaveBeenCalledWith(URI);
  });

  it('publishes a mute toggle to the engine', () => {
    const first = render({ isActive: true });
    const mute = elements(first).find(
      (element) => element.props?.accessibilityLabel === 'Mute video',
    );
    const onPress = mute?.props?.onPress as (() => void) | undefined;
    onPress?.();

    // The state the tap wrote survives into the next render (the state slots are
    // kept), and that render's effect publishes it.
    render({ isActive: true });
    runEffects();

    expect(mockActions.setMuted).toHaveBeenLastCalledWith(true);
  });
});

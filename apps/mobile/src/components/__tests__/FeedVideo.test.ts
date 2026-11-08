import FeedVideo from '../FeedVideo';

/**
 * The video *surface* suite (KNOT-051).
 *
 * `FeedVideo` no longer owns a player: it registers a hole with the media engine,
 * asks the engine to play its media, and draws the card's chrome. So the engine is
 * stubbed (its calls are the contract under test) and React's primitives are
 * stubbed too, because this project has no renderer and calls components
 * directly.
 *
 * Effects are captured and run by the harness. State slots are kept between
 * renders, which is how a test observes what an effect wrote — the same pattern
 * the previous suite used.
 */
const URI = 'http://10.0.2.2:8080/stories/1/media/2/content';

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
  activeMediaId: null as string | null,
  uri: null as string | null,
  mode: 'inline' as 'inline' | 'fullscreen',
  playing: false,
  muted: true,
  loop: true,
  currentTime: 0,
  duration: 0,
  frame: null,
  ready: false,
  frameVisible: false,
  naturalSize: null as { readonly width: number; readonly height: number } | null,
};

jest.mock('../../context/MediaPlayerContext', () => ({
  useMediaPlayerActions: () => mockActions,
  useMediaPlayback: () => mockPlayback,
}));

let mockStateValues: unknown[] = [];
let mockStateCursor = 0;
let mockEffects: (() => void)[] = [];

/** Applies a functional state update, as React would. */
function mockResolveState(current: unknown, next: unknown): unknown {
  const update = next as (value: unknown) => unknown;
  return typeof next === 'function' ? update(current) : next;
}

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useRef: (initial: unknown) => ({ current: initial }),
  useState: (initial: unknown) => {
    const index = mockStateCursor;
    mockStateCursor += 1;
    if (mockStateValues[index] === undefined) {
      mockStateValues[index] =
        typeof initial === 'function' ? (initial as () => unknown)() : initial;
    }
    const setValue = (next: unknown): void => {
      mockStateValues[index] = mockResolveState(mockStateValues[index], next);
    };
    return [mockStateValues[index], setValue];
  },
  useEffect: (effect: () => void) => {
    mockEffects.push(effect);
  },
}));

jest.mock('react-native', () => ({
  Image: 'Image',
  Pressable: 'Pressable',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

jest.mock('phosphor-react-native', () => ({
  SpeakerHigh: () => null,
  SpeakerSlash: () => null,
}));

type Element = { type?: unknown; props?: Record<string, unknown> };

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
 * Renders the surface with the hook cursors back at slot zero, keeping whatever
 * the previous render's effects wrote. Effects run after the tree is built, so a
 * test that observes state they wrote renders twice.
 */
function render(props: Parameters<typeof FeedVideo>[0]): Element {
  mockStateCursor = 0;
  mockEffects = [];
  const tree = FeedVideo(props) as Element;
  for (const effect of mockEffects) {
    effect();
  }
  return tree;
}

/** Renders twice, so a state an effect wrote is visible. */
function renderSettled(props: Parameters<typeof FeedVideo>[0]): Element {
  render(props);
  return render(props);
}

/** The progress badge's text, or the empty string when no badge is drawn. */
function badgeText(tree: unknown): string {
  for (const element of elements(tree)) {
    const children = element.props?.children;
    if (element.type === 'Text' && typeof children === 'string' && children.includes(' / ')) {
      return children;
    }
  }
  return '';
}

/** The extracted still, or undefined when none is drawn. */
function poster(tree: unknown): Element | undefined {
  return elements(tree).find((element) => element.props?.testID === 'feed-video-poster');
}

/** A clip the engine is already playing. */
function playing(overrides: Partial<typeof mockPlayback> = {}): void {
  mockPlayback = {
    ...mockPlayback,
    activeMediaId: URI,
    uri: URI,
    mode: 'inline',
    playing: true,
    ...overrides,
  };
}

beforeEach(() => {
  jest.clearAllMocks();
  mockPlayback = {
    activeMediaId: null,
    uri: null,
    mode: 'inline',
    playing: false,
    muted: true,
    loop: true,
    currentTime: 0,
    duration: 0,
    frame: null,
    ready: false,
    frameVisible: false,
    naturalSize: null,
  };
  mockStateValues = [];
  mockStateCursor = 0;
  mockEffects = [];
});

describe('FeedVideo surface', () => {
  /**
   * The whole task in one assertion: a card draws a hole, not a player. The one
   * `<Video>` belongs to `MediaHost`, which is what removes the per-card decoder.
   */
  it('draws a measured hole and no player of its own', () => {
    const tree = render({ uri: URI, isVisible: true });
    const host = elements(tree).find((element) => element.props?.testID === 'feed-video-host');

    expect(host).toBeDefined();
    expect(host?.type).toBe('View');
    expect(host?.props?.pointerEvents).toBe('none');
    expect(typeof host?.props?.onLayout).toBe('function');
    expect(elements(tree).some((element) => element.type === 'Video')).toBe(false);
  });

  it('leaves the mute control tappable above the picture', () => {
    const mute = elements(render({ uri: URI, isVisible: true })).find(
      (element) => element.type === 'Pressable' && element.props?.accessibilityLabel !== undefined,
    );

    expect(mute).toBeDefined();
  });
});

describe('FeedVideo engine attachment', () => {
  it('registers its hole and asks the engine to play when it is on screen', () => {
    render({ uri: URI, isVisible: true });

    expect(mockActions.registerHost).toHaveBeenCalledWith(URI, null);
    expect(mockActions.playMedia).toHaveBeenCalledWith(
      expect.objectContaining({ id: URI, uri: URI, mode: 'inline' }),
    );
  });

  it('uses the media id it was given, so the viewer can find the same clip', () => {
    render({ uri: URI, mediaId: 'media-42', isVisible: true });

    expect(mockActions.playMedia).toHaveBeenCalledWith(expect.objectContaining({ id: 'media-42' }));
  });

  it('releases the surface and pauses its own clip when it leaves the screen', () => {
    playing();
    render({ uri: URI, isVisible: false });

    expect(mockActions.releaseHost).toHaveBeenCalledWith(URI);
    expect(mockActions.pauseMedia).toHaveBeenCalledWith(URI);
    expect(mockActions.playMedia).not.toHaveBeenCalled();
  });

  it('stays out of the engine while the viewer holds the surface full-screen', () => {
    playing({ mode: 'fullscreen' });
    render({ uri: URI, isVisible: true });

    expect(mockActions.releaseHost).toHaveBeenCalledWith(URI);
    expect(mockActions.playMedia).not.toHaveBeenCalled();
  });

  it('publishes the feed-wide mute setting to the one player', () => {
    playing();
    render({ uri: URI, isVisible: true, isMuted: false });

    expect(mockActions.setMuted).toHaveBeenCalledWith(false);
  });

  it('leaves the mute alone when this card is not the one playing', () => {
    render({ uri: URI, isVisible: true, isMuted: false });

    expect(mockActions.setMuted).not.toHaveBeenCalled();
  });

  it('re-measures its hole when it is laid out', () => {
    const tree = render({ uri: URI, isVisible: true });
    const host = elements(tree).find((element) => element.props?.testID === 'feed-video-host');
    const onLayout = host?.props?.onLayout as (() => void) | undefined;

    onLayout?.();

    expect(mockActions.measureHost).toHaveBeenCalled();
  });
});

describe('FeedVideo timestamp', () => {
  it('counts the engine position against the engine duration', () => {
    playing({ currentTime: 15.4, duration: 76 });
    const tree = render({ uri: URI, isVisible: true, durationMs: 2_000 });

    // The engine's metadata wins over the payload's seed once it is known.
    expect(badgeText(tree)).toBe('0:15 / 1:16');
  });

  it('seeds the badge from the payload before the clip has loaded', () => {
    playing({ duration: 0 });
    const tree = render({ uri: URI, isVisible: true, durationMs: 76_000 });

    expect(badgeText(tree)).toBe('0:00 / 1:16');
  });

  it('draws no badge before any duration is known', () => {
    playing({ duration: 0 });
    const tree = render({ uri: URI, isVisible: true });

    expect(badgeText(tree)).toBe('');
  });
});

describe('FeedVideo poster', () => {
  const POSTER = `${URI}?variant=poster`;

  it('draws the extracted still until the engine reports a frame', () => {
    playing({ frameVisible: false });
    const still = poster(render({ uri: URI, posterUri: POSTER, isVisible: true }));

    expect(still).toBeDefined();
    expect(still?.type).toBe('Image');
    expect(still?.props?.source).toEqual({ uri: POSTER });
    expect(still?.props?.resizeMode).toBe('cover');
  });

  it('retires the still once a frame has been drawn', () => {
    playing({ frameVisible: true });

    expect(poster(renderSettled({ uri: URI, posterUri: POSTER, isVisible: true }))).toBeUndefined();
  });

  it('draws no still when the server sent none', () => {
    playing();

    expect(poster(render({ uri: URI, isVisible: true }))).toBeUndefined();
    expect(poster(render({ uri: URI, posterUri: '   ', isVisible: true }))).toBeUndefined();
    expect(poster(render({ uri: URI, posterUri: null, isVisible: true }))).toBeUndefined();
  });
});

describe('FeedVideo natural size', () => {
  it("reports the engine's metadata to the caller, so the frame can be shaped", () => {
    const onLoadSize = jest.fn();
    playing({ naturalSize: { width: 300, height: 700 } });

    render({ uri: URI, isVisible: true, onLoadSize });

    expect(onLoadSize).toHaveBeenCalledWith(300, 700);
  });

  it('reports nothing until the clip has loaded', () => {
    const onLoadSize = jest.fn();
    playing({ naturalSize: null });

    render({ uri: URI, isVisible: true, onLoadSize });

    expect(onLoadSize).not.toHaveBeenCalled();
  });
});

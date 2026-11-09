import FeedVideo from '../FeedVideo';

/**
 * The inline player suite (KNOT-050).
 *
 * `FeedVideo` owns a real `<Video>` again, mounted inside the card so the platform
 * scrolls the picture with it. This suite holds the three rules the placement
 * depends on: the player exists only for the active card, the still is the base
 * layer whenever there is no picture, and the card still publishes its playhead so
 * the full-screen viewer continues instead of restarting.
 *
 * Effects are captured and run by the harness, and state slots are kept between
 * renders, which is how a test observes what an effect or an event wrote — this
 * project has no renderer and calls components directly.
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
  getSavedPosition: jest.fn(() => 0),
  reportProgress: jest.fn(),
  reportLoaded: jest.fn(),
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
/** The cleanups the last render's effects registered, as unmounting would run them. */
let mockCleanups: (() => void)[] = [];

/** Applies a functional state update, as React would. */
function mockResolveState(current: unknown, next: unknown): unknown {
  const update = next as (value: unknown) => unknown;
  return typeof next === 'function' ? update(current) : next;
}

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  // The card memoises its cached source; a direct call has no render to memoise
  // across, so the factory is simply applied.
  useMemo: (factory: () => unknown) => factory(),
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
    return undefined;
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

// The card owns a real player again (KNOT-050), so the native module is stubbed to
// a host component the direct-call harness can build and drive.
jest.mock('react-native-video', () => ({ __esModule: true, default: 'Video' }));

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
  mockCleanups = [];
  const tree = FeedVideo(props) as Element;
  for (const effect of mockEffects) {
    const cleanup = effect() as unknown;
    if (typeof cleanup === 'function') {
      mockCleanups.push(cleanup as () => void);
    }
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

/** The card's own player, or undefined when this card draws none. */
function playerOf(tree: unknown): Element | undefined {
  return elements(tree).find((element) => element.type === 'Video');
}

/** Reports a load on the player this card drew. */
function fireLoad(tree: unknown, duration: number): void {
  (playerOf(tree)?.props?.onLoad as ((data: { duration: number }) => void) | undefined)?.({
    duration,
  });
}

/** Reports a progress tick on the player this card drew. */
function fireProgress(tree: unknown, currentTime: number): void {
  (playerOf(tree)?.props?.onProgress as ((data: { currentTime: number }) => void) | undefined)?.({
    currentTime,
  });
}

/**
 * The mutable box the card handed its player as a `ref`.
 *
 * A host element keeps its ref beside its props, not inside them, so the harness
 * reads `element.ref` — the box where a real mount would put the native player, and
 * standing one in is how a re-seat is observed.
 */
function refBoxOf(tree: unknown): { current: unknown } {
  return (playerOf(tree) as unknown as { ref: { current: unknown } }).ref;
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
  // `clearAllMocks` keeps implementations, so the playhead a re-seat test set has to
  // be put back explicitly.
  mockActions.getCurrentTime.mockReturnValue(0);
  mockActions.getSavedPosition.mockReturnValue(0);
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
  mockCleanups = [];
});

describe('FeedVideo inline mounting', () => {
  /**
   * The whole task in one assertion: the card that is on screen owns a real player,
   * in its own layout, so the platform scrolls the picture with the card.
   */
  it('mounts its own player inside the card when the card is the active one', () => {
    const tree = render({ uri: URI, isVisible: true });
    const player = playerOf(tree);
    const surface = elements(tree).find(
      (element) => element.props?.testID === 'feed-video-surface',
    );

    expect(player).toBeDefined();
    expect(player?.props?.source).toEqual({ uri: URI });
    expect(player?.props?.paused).toBe(false);
    expect(player?.props?.muted).toBe(true);
    expect(player?.props?.resizeMode).toBe('cover');
    // A `TextureView` is an ordinary view: it stays inside the card's frame and
    // stacks with the chrome instead of punching through it (KNOT-048-fix2).
    expect(player?.props?.useTextureView).toBe(true);
    // The player is part of the card, and its tap target is the card itself.
    expect(surface?.props?.pointerEvents).toBe('none');
  });

  it('unmounts the player when the card is not the active one', () => {
    const tree = render({ uri: URI, isVisible: false });

    // No player, and no hole for one: the still is the media.
    expect(playerOf(tree)).toBeUndefined();
    expect(
      elements(tree).find((element) => element.props?.testID === 'feed-video-surface'),
    ).toBeUndefined();
  });

  it('leaves the theatre to the viewer, so two players never share a window', () => {
    playing({ mode: 'fullscreen' });
    const tree = render({ uri: URI, isVisible: true });

    expect(playerOf(tree)).toBeUndefined();
    expect(mockActions.playMedia).not.toHaveBeenCalled();
  });

  it('leaves the mute control tappable above the picture', () => {
    const mute = elements(render({ uri: URI, isVisible: true })).find(
      (element) => element.type === 'Pressable' && element.props?.accessibilityLabel !== undefined,
    );

    expect(mute).toBeDefined();
  });
});

/**
 * The player is inline, but the *shared* facts are still worth publishing: the
 * engine is what lets the full-screen viewer open on the second the card reached.
 */
describe('FeedVideo engine publication', () => {
  it('claims the clip in the engine when it mounts', () => {
    render({ uri: URI, isVisible: true });

    expect(mockActions.playMedia).toHaveBeenCalledWith(
      expect.objectContaining({ id: URI, uri: URI, mode: 'inline' }),
    );
  });

  it('uses the media id it was given, so the viewer can find the same clip', () => {
    render({ uri: URI, mediaId: 'media-42', isVisible: true });

    expect(mockActions.playMedia).toHaveBeenCalledWith(expect.objectContaining({ id: 'media-42' }));
  });

  it('never claims a clip for a card that draws no player', () => {
    render({ uri: URI, isVisible: false });

    expect(mockActions.playMedia).not.toHaveBeenCalled();
  });

  it('publishes its playhead, so the viewer opens on the same second', () => {
    const tree = render({ uri: URI, isVisible: true });
    fireProgress(tree, 12.5);

    expect(mockActions.reportProgress).toHaveBeenCalledWith(URI, 12.5);
  });

  it('publishes the clip length it read, so the transport is seeded', () => {
    const tree = render({ uri: URI, isVisible: true });
    fireLoad(tree, 45);

    expect(mockActions.reportLoaded).toHaveBeenCalledWith(URI, 45);
  });

  it('publishes the card mute setting to the engine as well', () => {
    render({ uri: URI, isVisible: true, isMuted: false });

    expect(mockActions.setMuted).toHaveBeenCalledWith(false);
  });

  it('leaves the mute alone when the card draws no player', () => {
    render({ uri: URI, isVisible: false, isMuted: false });

    expect(mockActions.setMuted).not.toHaveBeenCalled();
  });
});

describe('FeedVideo timestamp', () => {
  it('counts the player position against the player duration', () => {
    const props = { uri: URI, isVisible: true, durationMs: 2_000 };
    const tree = render(props);
    fireLoad(tree, 76);
    fireProgress(tree, 15.4);

    // The player's own metadata wins over the payload's seed once it is known.
    expect(badgeText(render(props))).toBe('0:15 / 1:16');
  });

  it('seeds the badge from the payload before the clip has loaded', () => {
    const tree = render({ uri: URI, isVisible: true, durationMs: 76_000 });

    expect(badgeText(tree)).toBe('0:00 / 1:16');
  });

  it('draws no badge before any duration is known', () => {
    const tree = render({ uri: URI, isVisible: true });

    expect(badgeText(tree)).toBe('');
  });
});

describe('FeedVideo poster', () => {
  const POSTER = `${URI}?variant=poster`;

  it('draws the extracted still as the base layer until the player has a frame', () => {
    const still = poster(render({ uri: URI, posterUri: POSTER, isVisible: true }));

    expect(still).toBeDefined();
    expect(still?.type).toBe('Image');
    expect(still?.props?.source).toEqual({ uri: POSTER });
    expect(still?.props?.resizeMode).toBe('cover');
  });

  it('draws the still on a card that draws no player at all', () => {
    // This is the memory bound the inline placement rests on: an off-screen card is
    // a picture and nothing else.
    const tree = render({ uri: URI, posterUri: POSTER, isVisible: false });

    expect(playerOf(tree)).toBeUndefined();
    expect(poster(tree)).toBeDefined();
  });

  it('retires the still once the player has drawn a frame', () => {
    const props = { uri: URI, posterUri: POSTER, isVisible: true };
    const tree = render(props);
    fireProgress(tree, 1);

    expect(poster(renderSettled(props))).toBeUndefined();
  });

  it('brings the still back when the card stops being the active one', () => {
    const playingProps = { uri: URI, posterUri: POSTER, isVisible: true };
    fireProgress(render(playingProps), 1);
    renderSettled(playingProps);

    // The player is gone, so the picture has to be back.
    expect(poster(render({ uri: URI, posterUri: POSTER, isVisible: false }))).toBeDefined();
  });

  it('draws no still when the server sent none', () => {
    expect(poster(render({ uri: URI, isVisible: true }))).toBeUndefined();
    expect(poster(render({ uri: URI, posterUri: '   ', isVisible: true }))).toBeUndefined();
    expect(poster(render({ uri: URI, posterUri: null, isVisible: true }))).toBeUndefined();
  });
});

/**
 * The one thing an inline player must not lose is continuity: a card that mounts
 * again — the viewer was closed over it, or it was scrolled away long enough to be
 * unmounted — picks the clip up where it was, instead of restarting at 0:00.
 */
describe('FeedVideo re-seat', () => {
  it('seeks to the position saved for this media when its player mounts', () => {
    const seek = jest.fn();
    // The engine's own playhead is a different clip's second (0), so a card that
    // read it would start at the wrong place; the saved position is this media's.
    mockActions.getSavedPosition.mockReturnValue(19);
    const tree = render({ uri: URI, isVisible: true });
    refBoxOf(tree).current = { seek };

    fireLoad(tree, 45);

    expect(seek).toHaveBeenCalledWith(19);
  });

  /**
   * The bug, pinned: the engine's single playhead was at 0:05 because the previous
   * card had reached it. This card has never played, so it must open at 0:00 — the
   * previous clip's second may not leak into it (KNOT-051).
   */
  it('does not inherit the playhead of the card before it', () => {
    const seek = jest.fn();
    mockActions.getCurrentTime.mockReturnValue(5);
    mockActions.getSavedPosition.mockReturnValue(0);
    const tree = render({ uri: URI, isVisible: true });
    refBoxOf(tree).current = { seek };

    fireLoad(tree, 45);

    expect(mockActions.getSavedPosition).toHaveBeenCalledWith(URI);
    expect(seek).not.toHaveBeenCalled();
  });

  it('seeks nowhere for a clip that starts at zero', () => {
    const seek = jest.fn();
    const tree = render({ uri: URI, isVisible: true });
    refBoxOf(tree).current = { seek };

    fireLoad(tree, 45);

    expect(seek).not.toHaveBeenCalled();
  });
});

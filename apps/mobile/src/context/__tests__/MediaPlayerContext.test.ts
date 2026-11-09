import { MediaPlayerProvider } from '../MediaPlayerContext';
import type { MediaPlayerState } from '../mediaPlayerState';

/**
 * The media player context's imperative API (KNOT-051).
 *
 * The provider owns the reducer and the one native ref; this suite drives its
 * methods with a stubbed dispatch, so the task's required surface —
 * `playMedia`, `pauseMedia`, `getCurrentTime`, `getActiveMediaId` — is pinned
 * without a renderer. The transitions themselves are covered exhaustively in
 * `mediaPlayerState.test.ts`; this is the wiring.
 */
let mockState: MediaPlayerState;
const mockDispatch = jest.fn();

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useReducer: () => [mockState, mockDispatch],
  useRef: (initial: unknown) => ({ current: initial }),
  useCallback: (callback: unknown) => callback,
  useMemo: (factory: () => unknown) => factory(),
  // The provider re-measures its host after every committed transition; there is
  // no renderer in this suite, so the effect is simply not run.
  useEffect: () => undefined,
}));

jest.mock('../../theme', () => ({
  useThemedStyles: () => ({ root: {} }),
}));

jest.mock('react-native', () => ({
  StyleSheet: { create: (styles: unknown) => styles },
  View: 'View',
}));

jest.mock('react-native-video', () => ({ __esModule: true, default: 'Video' }));

jest.mock('../../utils/videoCache', () => ({
  cachedVideoUri: (url: string) => `cached:${url}`,
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

type Actions = {
  playMedia: (request: unknown) => void;
  pauseMedia: (id?: string) => void;
  resumeMedia: (id?: string) => void;
  togglePlayback: (id?: string) => void;
  seekTo: (seconds: number) => void;
  setMuted: (muted: boolean) => void;
  clearSurface: (id?: string) => void;
  releaseMedia: () => void;
  getCurrentTime: () => number;
  getActiveMediaId: () => string | null;
  getSavedPosition: (mediaId: string) => number;
};

/** The actions object the provider publishes. */
function actions(): Actions {
  const tree = MediaPlayerProvider({ children: null }) as Element;

  for (const element of elements(tree)) {
    const value = element.props?.value as Partial<Actions> | undefined;
    if (value !== undefined && typeof value.playMedia === 'function') {
      return value as Actions;
    }
  }

  throw new Error('the provider published no media player actions');
}

beforeEach(() => {
  jest.clearAllMocks();
  mockState = {
    activeMediaId: 'media-1',
    uri: 'http://api/stories/1/media/1/content',
    mode: 'inline',
    playing: true,
    muted: true,
    loop: true,
    currentTime: 19,
    duration: 45,
    savedPositions: { 'media-1': 19 },
    frame: { x: 0, y: 100, width: 390, height: 220 },
    ready: true,
    frameVisible: true,
    naturalSize: { width: 1080, height: 1920 },
  };
});

describe('MediaPlayerContext API', () => {
  it('plays a media by dispatching a play action', () => {
    const request = {
      id: 'media-1',
      uri: 'http://api/stories/1/media/1/content',
      mode: 'fullscreen',
    };

    actions().playMedia(request);

    expect(mockDispatch).toHaveBeenCalledWith({ type: 'play', request });
  });

  it('seeks immediately when the clip is already loaded and must jump', () => {
    actions().playMedia({
      id: 'media-1',
      uri: 'http://api/stories/1/media/1/content',
      startTime: 30,
    });

    // The play transition, then the seek that lands on the requested second.
    expect(mockDispatch).toHaveBeenNthCalledWith(1, expect.objectContaining({ type: 'play' }));
    expect(mockDispatch).toHaveBeenNthCalledWith(2, {
      type: 'seek',
      id: 'media-1',
      currentTime: 30,
    });
  });

  it('pauses the media it is asked to pause', () => {
    actions().pauseMedia('media-1');

    expect(mockDispatch).toHaveBeenCalledWith({ type: 'pause', id: 'media-1' });
  });

  it('hides and releases the surface through the engine', () => {
    const player = actions();

    player.clearSurface('media-1');
    player.releaseMedia();

    expect(mockDispatch).toHaveBeenCalledWith({ type: 'clearSurface', id: 'media-1' });
    expect(mockDispatch).toHaveBeenCalledWith({ type: 'release' });
  });

  it('keeps one mute setting', () => {
    actions().setMuted(false);

    expect(mockDispatch).toHaveBeenCalledWith({ type: 'muted', muted: false });
  });

  it('reads the playhead and the active media live, not from a render', () => {
    const player = actions();

    expect(player.getCurrentTime()).toBe(19);
    expect(player.getActiveMediaId()).toBe('media-1');
  });

  it('answers a position per media, never one global playhead', () => {
    const player = actions();

    // The engine is holding 19 seconds of `media-1`. Asking about another media
    // must not borrow it — that is the bleed this API removes (KNOT-051).
    expect(player.getSavedPosition('media-1')).toBe(19);
    expect(player.getSavedPosition('media-2')).toBe(0);
    expect(player.getSavedPosition('   ')).toBe(0);
  });

  it('refuses a seek that is not a real position', () => {
    const player = actions();

    player.seekTo(Number.NaN);
    player.seekTo(-4);

    expect(mockDispatch).not.toHaveBeenCalled();
  });

  it('rejects a play with no identity', () => {
    actions().playMedia({ id: '  ', uri: 'http://api/x' });

    expect(mockDispatch).toHaveBeenCalledWith({
      type: 'play',
      request: { id: '  ', uri: 'http://api/x' },
    });
    // The reducer is what refuses it; the provider must still pass it through.
    expect(mockDispatch).toHaveBeenCalledTimes(1);
  });
});

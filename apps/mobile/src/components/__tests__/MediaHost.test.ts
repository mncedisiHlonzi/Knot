import MediaHost from '../MediaHost';
import type { MediaSurfaceHandle } from '../../context/MediaPlayerContext';

/**
 * The one host (KNOT-051).
 *
 * `MediaHost` is the only component that renders a `<Video>`. This suite holds it
 * to that contract: nothing is drawn without a frame, only one placement draws at
 * a time, the player is intangible and positioned from the engine's rectangle, and
 * a host re-seated mid-clip resumes at the engine's playhead instead of 0:00.
 */
const mockPlayer = {
  getCurrentTime: jest.fn(() => 0),
};

let mockSurface: MediaSurfaceHandle;

jest.mock('../../context/MediaPlayerContext', () => ({
  useMediaSurface: () => mockSurface,
  useMediaPlayerActions: () => mockPlayer,
}));

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useRef: (initial: unknown) => ({ current: initial }),
}));

jest.mock('react-native', () => ({
  StyleSheet: { create: (styles: unknown) => styles },
  View: 'View',
}));

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

/** A surface handle with a full-screen video. */
function surface(overrides: Partial<MediaSurfaceHandle> = {}): MediaSurfaceHandle {
  return {
    frame: { x: 0, y: 0, width: 390, height: 844 },
    mode: 'fullscreen',
    uri: 'http://api/stories/1/media/1/content',
    paused: false,
    muted: false,
    loop: true,
    videoRef: { current: null },
    onLoad: jest.fn(),
    onProgress: jest.fn(),
    ...overrides,
  };
}

/** Calls the host and expands the surface view it returns. */
function draw(placement: 'root' | 'viewer' = 'viewer'): Element | null {
  const host = MediaHost({ placement }) as Element | null;
  if (host === null) {
    return null;
  }

  return (host.type as (props: unknown) => Element)(host.props);
}

beforeEach(() => {
  jest.clearAllMocks();
  mockPlayer.getCurrentTime.mockReturnValue(0);
  mockSurface = surface();
});

describe('MediaHost placement', () => {
  it('draws nothing until a surface has a rectangle', () => {
    mockSurface = surface({ frame: null });

    expect(draw('root')).toBeNull();
  });

  it('draws nothing until a clip is playing', () => {
    mockSurface = surface({ uri: null });

    expect(draw('root')).toBeNull();
  });

  it('lets only the matching placement draw', () => {
    mockSurface = surface({ mode: 'inline' });

    expect(draw('viewer')).toBeNull();
    expect(draw('root')).not.toBeNull();
  });
});

describe('MediaHost player', () => {
  it('positions one intangible player at the reported rectangle', () => {
    mockSurface = surface({
      frame: { x: 16, y: 240, width: 358, height: 201 },
      mode: 'inline',
    });
    const tree = draw('root');
    const box = elements(tree).find((element) => element.props?.testID === 'media-host-root');
    const video = elements(tree).find((element) => element.type === 'Video');

    expect(box?.props?.pointerEvents).toBe('none');
    expect(box?.props?.style).toContainEqual(
      expect.objectContaining({ height: 201, left: 16, top: 240, width: 358 }),
    );
    expect(video).toBeDefined();
    expect(video?.props?.paused).toBe(false);
    expect(video?.props?.muted).toBe(false);
    expect(video?.props?.repeat).toBe(true);
  });

  it('fills an inline frame and letterboxes the theatre', () => {
    mockSurface = surface({ mode: 'inline' });
    expect(
      elements(draw('root')).find((element) => element.type === 'Video')?.props?.resizeMode,
    ).toBe('cover');

    mockSurface = surface({ mode: 'fullscreen' });
    expect(
      elements(draw('viewer')).find((element) => element.type === 'Video')?.props?.resizeMode,
    ).toBe('contain');
  });

  it('resumes at the engine playhead when it is mounted mid-clip', () => {
    const seek = jest.fn();
    mockPlayer.getCurrentTime.mockReturnValue(19);
    mockSurface = surface({ videoRef: { current: { seek } as never } });
    const tree = draw('viewer');
    const video = elements(tree).find((element) => element.type === 'Video');
    const onLoad = video?.props?.onLoad as (data: unknown) => void;

    onLoad({ duration: 45, naturalSize: { width: 1920, height: 1080 } });

    expect(mockSurface.onLoad).toHaveBeenCalledWith({
      duration: 45,
      naturalSize: { width: 1920, height: 1080 },
    });
    // The re-seat is seamless: the modal's new native player picks up at 19s.
    expect(seek).toHaveBeenCalledWith(19);

    // Only the first load seeks; a later one must not rewind a playing clip.
    onLoad({ duration: 45 });
    expect(seek).toHaveBeenCalledTimes(1);
  });

  it('issues no seek for a clip that starts at zero', () => {
    const seek = jest.fn();
    mockSurface = surface({ videoRef: { current: { seek } as never } });
    const video = elements(draw('viewer')).find((element) => element.type === 'Video');
    const onLoad = video?.props?.onLoad as (data: unknown) => void;

    onLoad({ duration: 45 });

    expect(seek).not.toHaveBeenCalled();
  });
});

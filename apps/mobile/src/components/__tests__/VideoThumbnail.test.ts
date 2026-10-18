import VideoThumbnail from '../VideoThumbnail';

// VideoThumbnail uses one hook (the "first frame is ready" flag), so the project's
// renderer-less pattern stubs the hook primitives and calls the component as a
// plain function, exactly as the FeedEntry suite does. The initial state is the
// pre-load state, which is the one a static call can honestly assert.
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useState: (initial: unknown) => [initial, () => undefined],
}));

jest.mock('react-native', () => ({
  StyleSheet: {
    // The real shape, so the anchor's props can be asserted as they are on device.
    absoluteFillObject: { bottom: 0, left: 0, position: 'absolute', right: 0, top: 0 },
    create: (styles: unknown) => styles,
  },
  View: 'View',
}));

jest.mock('react-native-video', () => ({ __esModule: true, default: 'Video' }));

jest.mock('phosphor-react-native', () => ({ Play: 'Play' }));

/** Finds the first element of the given type in a directly-called tree. */
function findElementByType(
  node: unknown,
  type: unknown,
): { props?: Record<string, unknown> } | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findElementByType(child, type);
      if (found !== undefined) {
        return found;
      }
    }
    return undefined;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return undefined;
  }

  const element = node as { type?: unknown; props?: { children?: unknown } };
  if (element.type === type) {
    return element as { props?: Record<string, unknown> };
  }
  if (element.props !== undefined) {
    return findElementByType(element.props.children, type);
  }
  return undefined;
}

/** Finds the first element whose props satisfy the predicate. */
function findElement(
  node: unknown,
  predicate: (props: Record<string, unknown>) => boolean,
): { props?: Record<string, unknown> } | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findElement(child, predicate);
      if (found !== undefined) {
        return found;
      }
    }
    return undefined;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return undefined;
  }

  const element = node as { props?: Record<string, unknown>; children?: unknown };
  if (element.props !== undefined && predicate(element.props)) {
    return element;
  }
  return findElement(element.props?.children, predicate);
}

const URI = 'https://cdn.example/stories/s1/m4';

describe('VideoThumbnail', () => {
  it('draws a paused, muted video as the static poster surface', () => {
    const element = VideoThumbnail({ uri: URI });
    const video = findElementByType(element, 'Video');

    expect(video).toBeDefined();
    expect(video?.props?.source).toEqual({ uri: URI });
    expect(video?.props?.paused).toBe(true);
    expect(video?.props?.muted).toBe(true);
    expect(video?.props?.repeat).toBe(false);
    expect(video?.props?.resizeMode).toBe('cover');
    // Android: a TextureView stays inside the tile's clip.
    expect(video?.props?.useTextureView).toBe(true);
    // Loading, and the first rendered frame, both retire the placeholder.
    expect(typeof video?.props?.onLoad).toBe('function');
    expect(typeof video?.props?.onReadyForDisplay).toBe('function');
  });

  it('sizes the frame and the player explicitly, so the surface cannot measure itself', () => {
    const element = VideoThumbnail({ uri: URI, size: 64 });
    const video = findElementByType(element, 'Video');

    // The square comes from the frame's own width/height (KNOT-025-fix5)...
    expect(element.props?.style).toEqual(expect.arrayContaining([{ height: 64, width: 64 }]));
    // ...and the player only ever fills it, so it cannot grow past the tile.
    expect(video?.props?.style).toEqual({ height: '100%', width: '100%' });
  });

  it('defaults the frame to a 64-point square', () => {
    const element = VideoThumbnail({ uri: URI });

    expect(element.props?.style).toEqual(expect.arrayContaining([{ height: 64, width: 64 }]));
  });

  it('clips the player inside an absolute-fill anchor within the fixed square', () => {
    const element = VideoThumbnail({ uri: URI, size: 64 });
    const frameStyles = element.props?.style as Array<Record<string, unknown>>;

    // The outermost wrapper is the explicit square that owns the clip (fix6).
    expect(frameStyles[1]).toEqual({ height: 64, width: 64 });
    expect(frameStyles[0]).toMatchObject({ overflow: 'hidden' });

    const anchor = findElement(element, (props) => {
      const style = props.style as Record<string, unknown> | undefined;

      return (
        style?.position === 'absolute' &&
        style.top === 0 &&
        style.bottom === 0 &&
        style.left === 0 &&
        style.right === 0 &&
        // Excludes the absolute placeholder, which paints a fill.
        style.backgroundColor === undefined
      );
    });

    // The player only ever lays out inside that anchor...
    expect(anchor).toBeDefined();
    expect(findElementByType(anchor?.props?.children, 'Video')).toBeDefined();
    // ...while the overlays stay siblings of it, inside the clipped frame.
    expect(findElementByType(anchor?.props?.children, 'Play')).toBeUndefined();
    expect(findElementByType(element, 'Play')).toBeDefined();
  });

  it('covers the tile with a sunken placeholder until the first frame loads', () => {
    const element = VideoThumbnail({ uri: URI });
    const fallback = findElement(element, (props) => props.testID === 'video-thumbnail-fallback');

    expect(fallback).toBeDefined();
  });

  it('marks the tile with a play glyph', () => {
    const element = VideoThumbnail({ uri: URI });

    expect(findElementByType(element, 'Play')).toBeDefined();
  });
});

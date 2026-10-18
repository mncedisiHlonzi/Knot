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
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

jest.mock('react-native-video', () => ({ __esModule: true, default: 'Video' }));

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

/** Collects every string child in a directly-called tree. */
function collectText(node: unknown, found: string[] = []): string[] {
  if (Array.isArray(node)) {
    for (const child of node) {
      collectText(child, found);
    }
    return found;
  }
  if (typeof node === 'string') {
    found.push(node);
    return found;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return found;
  }

  const element = node as { props?: { children?: unknown } };
  if (element.props !== undefined) {
    collectText(element.props.children, found);
  }

  return found;
}

const URI = 'https://cdn.example/stories/s1/m4';

describe('VideoThumbnail', () => {
  it('pauses a muted looping video when it is not the visible item', () => {
    const element = VideoThumbnail({ uri: URI, isVisible: false });
    const video = findElementByType(element, 'Video');

    expect(video).toBeDefined();
    expect(video?.props?.source).toEqual({ uri: URI });
    expect(video?.props?.paused).toBe(true);
    expect(video?.props?.muted).toBe(true);
    expect(video?.props?.repeat).toBe(true);
    expect(video?.props?.resizeMode).toBe('cover');
    // Android: a TextureView stays inside the tile's clip.
    expect(video?.props?.useTextureView).toBe(true);
    // Loading, and the first rendered frame, both retire the placeholder.
    expect(typeof video?.props?.onLoad).toBe('function');
    expect(typeof video?.props?.onReadyForDisplay).toBe('function');
  });

  it('plays the active item automatically, still muted', () => {
    const element = VideoThumbnail({ uri: URI, isVisible: true });
    const video = findElementByType(element, 'Video');

    expect(video?.props?.paused).toBe(false);
    expect(video?.props?.muted).toBe(true);
  });

  it('covers the tile with a sunken placeholder until the first frame loads', () => {
    const element = VideoThumbnail({ uri: URI, isVisible: true });
    const fallback = findElement(element, (props) => props.testID === 'video-thumbnail-fallback');

    expect(fallback).toBeDefined();
  });

  it('draws the payload duration on the tile', () => {
    const element = VideoThumbnail({ uri: URI, isVisible: true, durationMs: 48000 });

    expect(collectText(element)).toContain('0:48');
  });

  it('draws no duration badge when the payload has none', () => {
    const element = VideoThumbnail({ uri: URI, isVisible: true, durationMs: null });

    expect(collectText(element)).not.toContain('0:00');
  });
});

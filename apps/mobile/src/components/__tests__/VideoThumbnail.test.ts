import { colors } from '../../theme';
import VideoThumbnail from '../VideoThumbnail';

// VideoThumbnail is now a plain, hookless function (KNOT-025-fix9), so it can be
// called directly under the project's renderer-less pattern. Only the exports it
// touches are mocked.
jest.mock('react-native', () => ({
  // The component must NOT rely on `absoluteFillObject`; leaving it out of this
  // mock makes a regression there fail loudly (KNOT-025-fix8).
  StyleSheet: {
    create: (styles: unknown) => styles,
  },
  Image: 'Image',
  View: 'View',
}));

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

const POSTER_URI = 'https://cdn.example/stories/s1/m4/poster.jpg';

describe('VideoThumbnail', () => {
  it('draws the poster image when one is provided', () => {
    const element = VideoThumbnail({ posterUrl: POSTER_URI });
    const image = findElementByType(element, 'Image');

    expect(image).toBeDefined();
    expect(image?.props?.source).toEqual({ uri: POSTER_URI });
    expect(image?.props?.resizeMode).toBe('cover');
    // The still fills the strict square (KNOT-025-fix9).
    expect(image?.props?.style).toEqual({ height: '100%', width: '100%' });
    // No live player is rendered anywhere (KNOT-025-fix9).
    expect(findElementByType(element, 'Video')).toBeUndefined();
  });

  it('draws a sunken placeholder when no poster is available', () => {
    const element = VideoThumbnail({});
    const placeholder = findElement(
      element,
      (props) => props.testID === 'video-thumbnail-placeholder',
    );

    expect(findElementByType(element, 'Image')).toBeUndefined();
    expect(placeholder).toBeDefined();
    expect(placeholder?.props?.style).toEqual({
      backgroundColor: colors.bg.sunken,
      height: '100%',
      width: '100%',
    });
  });

  it('defaults the frame to a 64-point square with clipping and no flex', () => {
    const element = VideoThumbnail({});
    const frameStyles = element.props?.style as Array<Record<string, unknown>>;

    expect(frameStyles[1]).toEqual({ height: 64, width: 64 });
    expect(frameStyles[0]).toMatchObject({ overflow: 'hidden' });
    // The fix8 constraints are preserved (KNOT-025-fix9).
    expect(frameStyles[0]).not.toHaveProperty('flex');
    expect(frameStyles[0]).not.toHaveProperty('flexGrow');
    expect(frameStyles[0]).not.toHaveProperty('flexShrink');
  });

  it('sizes a custom square explicitly', () => {
    const element = VideoThumbnail({ size: 48 });

    expect(element.props?.style).toEqual(expect.arrayContaining([{ height: 48, width: 48 }]));
  });

  it('keeps the still media inside the explicit absolute anchor', () => {
    const element = VideoThumbnail({ posterUrl: POSTER_URI, size: 64 });
    const anchor = findElement(element, (props) => {
      const style = props.style as Record<string, unknown> | undefined;

      return (
        style?.position === 'absolute' &&
        style.top === 0 &&
        style.left === 0 &&
        style.width === 64 &&
        style.height === 64 &&
        // Excludes the stationary filled layers.
        style.backgroundColor === undefined
      );
    });

    expect(anchor).toBeDefined();
    // Explicit dimensions, NOT absoluteFillObject's four insets (fix8).
    expect(anchor?.props?.style).toEqual({
      height: 64,
      left: 0,
      position: 'absolute',
      top: 0,
      width: 64,
    });
    expect(anchor?.props?.style).not.toHaveProperty('right');
    expect(anchor?.props?.style).not.toHaveProperty('bottom');

    expect(findElementByType(anchor?.props?.children, 'Image')).toBeDefined();
    // The play disc is a sibling overlay, not part of the media box.
    expect(findElementByType(anchor?.props?.children, 'Play')).toBeUndefined();
    expect(findElementByType(element, 'Play')).toBeDefined();
  });

  it('marks the tile with a play glyph whether or not a poster exists', () => {
    expect(findElementByType(VideoThumbnail({ posterUrl: POSTER_URI }), 'Play')).toBeDefined();
    expect(findElementByType(VideoThumbnail({}), 'Play')).toBeDefined();
  });
});

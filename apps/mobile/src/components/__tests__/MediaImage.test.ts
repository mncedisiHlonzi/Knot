import MediaImage from '../MediaImage';

/**
 * The one image renderer (KNOT-051).
 *
 * Every surface used to draw its own layers; they now share this component, so
 * the suite pins the contract they all rely on: a still underneath, a full
 * rendition over it, blank addresses treated as absent, and the decoded size
 * reported back so a frame can settle.
 */
jest.mock('../../theme', () => ({
  motion: { durations: { medium: 250 }, easing: { standard: [0.2, 0, 0, 1] } },
}));

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useCallback: (callback: unknown) => callback,
  useRef: (initial: unknown) => ({ current: initial }),
}));

jest.mock('react-native', () => ({
  Animated: {
    Image: 'Animated.Image',
    Value: class {
      setValue(): void {
        return undefined;
      }
    },
    timing: () => ({ start: () => undefined }),
  },
  Easing: { bezier: () => undefined },
  Image: 'Image',
  StyleSheet: { create: (styles: unknown) => styles },
  View: 'View',
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

const FULL = 'http://api/stories/1/media/1/content?variant=display&format=webp';
const STILL = 'http://api/stories/1/media/1/content?variant=thumb';

describe('MediaImage layers', () => {
  it('draws the still first and the full rendition over it', () => {
    const tree = MediaImage({ uri: FULL, stillUri: STILL }) as Element;
    const images = elements(tree).filter(
      (element) => element.type === 'Image' || element.type === 'Animated.Image',
    );

    expect(images.map((element) => element.type)).toEqual(['Image', 'Animated.Image']);
    expect(images[0]?.props?.source).toEqual({ uri: STILL });
    expect(images[1]?.props?.source).toEqual({ uri: FULL });
    // The default test ids are what a caller that names none gets.
    expect(images[0]?.props?.testID).toBe('media-image-still');
    expect(images[1]?.props?.testID).toBe('media-image-full');
  });

  it('uses the test ids a surface asks for, so the viewer keeps its own', () => {
    const tree = MediaImage({
      uri: FULL,
      stillUri: STILL,
      stillTestID: 'viewer-still',
      fullTestID: 'viewer-full-rendition',
    }) as Element;
    const ids = elements(tree)
      .map((element) => element.props?.testID)
      .filter((id): id is string => typeof id === 'string');

    expect(ids).toEqual(['viewer-still', 'viewer-full-rendition']);
  });

  it('draws a single layer when there is no still', () => {
    for (const stillUri of [null, undefined, '', '   ']) {
      const tree = MediaImage({ uri: FULL, stillUri }) as Element;
      const images = elements(tree).filter(
        (element) => element.type === 'Image' || element.type === 'Animated.Image',
      );

      expect(images).toHaveLength(1);
      expect(images[0]?.type).toBe('Animated.Image');
    }
  });

  it('draws nothing for a blank address rather than fetching the base URL', () => {
    expect(MediaImage({ uri: '   ' })).toBeNull();
    expect(MediaImage({ uri: '' })).toBeNull();
  });
});

describe('MediaImage load', () => {
  it('reports the decoded size and then retires a loader', () => {
    const onLoadSize = jest.fn();
    const onFullLoadEnd = jest.fn();
    const tree = MediaImage({ uri: FULL, onLoadSize, onFullLoadEnd }) as Element;
    const full = elements(tree).find((element) => element.type === 'Animated.Image');
    const onLoad = full?.props?.onLoad as (event: unknown) => void;

    onLoad({ nativeEvent: { source: { width: 300, height: 700 } } });

    expect(onLoadSize).toHaveBeenCalledWith(300, 700);
    expect(onFullLoadEnd).toHaveBeenCalledTimes(1);
  });

  it('ignores a decoded size that is not a shape', () => {
    const onLoadSize = jest.fn();
    const tree = MediaImage({ uri: FULL, onLoadSize }) as Element;
    const full = elements(tree).find((element) => element.type === 'Animated.Image');
    const onLoad = full?.props?.onLoad as (event: unknown) => void;

    onLoad({ nativeEvent: { source: { width: 0, height: 700 } } });

    expect(onLoadSize).not.toHaveBeenCalled();
  });
});

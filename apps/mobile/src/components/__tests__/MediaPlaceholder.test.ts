import MediaPlaceholder, { LQIP_BLUR_RADIUS } from '../MediaPlaceholder';

/**
 * The placeholder is a hook-bearing component, and this project tests components
 * by CALLING them (there is no renderer). The primitives it needs are stubbed:
 * effects are skipped, which is exactly what a static call can honestly assert —
 * the pulse's target opacity is render output, the animation itself is not.
 */
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../theme/ThemeContext').lightTheme,
  // A direct call runs outside a render, so `useRef` would have no dispatcher and
  // `useEffect` would never run; both are stubbed to what a single static render
  // can honestly produce (KNOT-043).
  useEffect: () => undefined,
  useRef: (initial: unknown) => ({ current: initial }),
}));

jest.mock('react-native', () => {
  /** The smallest Animated.Value the component reads: it interpolates. */
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
      loop: () => ({ start: () => undefined, stop: () => undefined }),
      sequence: () => ({}),
      timing: () => ({ start: () => undefined }),
    },
    Easing: { bezier: () => undefined },
    Image: 'Image',
    StyleSheet: { absoluteFillObject: {}, create: (styles: unknown) => styles },
    Text: 'Text',
    View: 'View',
  };
});

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

/** The first element carrying a test identifier. */
function findByTestID(tree: unknown, testID: string): Element | undefined {
  return elements(tree).find((element) => element.props?.testID === testID);
}

/** The text content a direct call produced. */
function textContent(tree: unknown): string {
  return elements(tree)
    .filter((element) => element.type === 'Text')
    .map((element) => element.props?.children)
    .filter((child): child is string => typeof child === 'string')
    .join(' ');
}

describe('MediaPlaceholder LQIP', () => {
  const LQIP = 'http://localhost:8080/stories/s/media/m/content?variant=lqip';

  /**
   * The whole point of the component: a ~1KB picture on screen immediately, so a
   * slow frame is a blurred photograph rather than white (KNOT-043).
   */
  it('draws the placeholder picture stretched over the frame, blurred', () => {
    const picture = findByTestID(MediaPlaceholder({ lqipUri: LQIP }), 'media-placeholder-lqip');

    expect(picture).toBeDefined();
    expect(picture?.type).toBe('Image');
    expect(picture?.props?.source).toEqual({ uri: LQIP });
    expect(picture?.props?.resizeMode).toBe('cover');
    expect(picture?.props?.blurRadius).toBe(LQIP_BLUR_RADIUS);
  });

  it('draws the animated skeleton when the row has no placeholder', () => {
    for (const lqipUri of [undefined, null, '', '   ']) {
      const tree = MediaPlaceholder({ lqipUri });

      expect(findByTestID(tree, 'media-placeholder-lqip')).toBeUndefined();
      expect(findByTestID(tree, 'media-placeholder-shimmer')).toBeDefined();
    }
  });

  it('never draws a picture in skeleton mode, even with a placeholder address', () => {
    const tree = MediaPlaceholder({ lqipUri: LQIP, variant: 'skeleton' });

    expect(findByTestID(tree, 'media-placeholder-lqip')).toBeUndefined();
    expect(findByTestID(tree, 'media-placeholder-shimmer')).toBeDefined();
  });

  it('overlays a spinner while the frame is busy', () => {
    const busy = MediaPlaceholder({ lqipUri: LQIP });

    expect(elements(busy).some((element) => element.type === 'ActivityIndicator')).toBe(true);
    expect(busy.props?.accessibilityRole).toBe('progressbar');
  });

  it('draws no spinner when the caller only wants the still', () => {
    const quiet = MediaPlaceholder({ lqipUri: LQIP, spinner: false });

    expect(elements(quiet).some((element) => element.type === 'ActivityIndicator')).toBe(false);
  });

  it('labels the wait, and announces the same words', () => {
    const tree = MediaPlaceholder({ variant: 'skeleton', label: 'Processing media...' });

    expect(textContent(tree)).toContain('Processing media...');
    expect(tree.props?.accessibilityLabel).toBe('Processing media...');
  });

  it('announces a generic label when the caller gave none', () => {
    expect(MediaPlaceholder({ lqipUri: LQIP }).props?.accessibilityLabel).toBe('Media loading');
  });
});

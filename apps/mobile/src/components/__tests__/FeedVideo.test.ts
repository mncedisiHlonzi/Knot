import FeedVideo from '../FeedVideo';

/**
 * `FeedVideo` holds the duration it learns on load, so it needs the hook
 * primitives stubbed before it can be CALLED directly — this project has no
 * renderer and must not add one. `useState` returns its initial value and
 * `useCallback` returns the callback unchanged; everything else comes from the
 * real `react`, with `useContext` serving the light theme, exactly as
 * `jest.setup.js` does for every other suite.
 */
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useState: (initial: unknown) => [
    typeof initial === 'function' ? (initial as () => unknown)() : initial,
    () => undefined,
  ],
}));

jest.mock('react-native', () => ({
  Pressable: 'Pressable',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

jest.mock('phosphor-react-native', () => ({
  SpeakerHigh: () => null,
  SpeakerSlash: () => null,
}));

// react-native-video is a native module: stub it to a host component so the
// element tree can be built and inspected.
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

/** The first element of the given host type. */
function firstOfType(tree: unknown, type: string): Element | undefined {
  return elements(tree).find((element) => element.type === type);
}

const TREE = FeedVideo({ uri: 'http://localhost:8080/stories/1/media/2/content', isVisible: true });

describe('FeedVideo touch handling', () => {
  /**
   * The regression this guards (KNOT-020k-fix5): on Android the native video
   * surface sits above the React view it is drawn in, and it consumed the press
   * before it could reach the Pressable every caller wraps this component in. The
   * card's video then did nothing at all while an image in the same card opened
   * the viewer.
   */
  it('keeps the player out of the touch system so the caller receives the tap', () => {
    const anchor = elements(TREE).find((element) => element.props?.pointerEvents === 'none');

    expect(anchor).toBeDefined();
    expect(anchor?.type).toBe('View');
    expect(firstOfType(anchor?.props?.children, 'Video')).toBeDefined();
  });

  it('leaves the mute control tappable above the player', () => {
    const mute = elements(TREE).find(
      (element) => element.type === 'Pressable' && element.props?.accessibilityLabel !== undefined,
    );

    expect(mute).toBeDefined();
  });
});

describe('FeedVideo playback', () => {
  it('plays the visible video and loops it', () => {
    const video = firstOfType(TREE, 'Video');
    const paused = video?.props?.paused;

    expect(paused).toBe(false);
    expect(video?.props?.repeat).toBe(true);
  });

  it('pauses the video that is off screen', () => {
    const tree = FeedVideo({
      uri: 'http://localhost:8080/stories/1/media/2/content',
      isVisible: false,
    });
    const video = firstOfType(tree, 'Video');

    expect(video?.props?.paused).toBe(true);
  });
});

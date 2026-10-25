import FeedVideo from '../FeedVideo';

/**
 * The state each hook slot holds, in call order, plus the cursor that maps a
 * render onto those slots. They are module-scope `mock*` bindings so the factory
 * below may reference them (babel-jest rejects any other out-of-scope name) and
 * so a test can re-render the component and observe the state a callback wrote —
 * which is how the progress badge is asserted. Everything is read lazily inside
 * the hook, never while the factory runs, so there is no TDZ hazard.
 */
let mockStateValues: unknown[] = [];
let mockStateCursor = 0;

/**
 * `FeedVideo` holds the duration it learns on load and the position its player
 * reports, so it needs the hook primitives stubbed before it can be CALLED
 * directly — this project has no renderer and must not add one. Everything else
 * comes from the real `react`, with `useContext` serving the light theme, exactly
 * as `jest.setup.js` does for every other suite.
 */
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useState: (initial: unknown) => {
    const index = mockStateCursor;
    mockStateCursor += 1;
    if (mockStateValues[index] === undefined) {
      mockStateValues[index] =
        typeof initial === 'function' ? (initial as () => unknown)() : initial;
    }
    const setValue = (next: unknown): void => {
      mockStateValues[index] = next;
    };
    return [mockStateValues[index], setValue];
  },
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

/** The video's URL, in the shape every test in this suite uses. */
const URI = 'http://localhost:8080/stories/1/media/2/content';

/**
 * Renders the component with the hook cursor back at slot zero, keeping whatever
 * the previous render's callbacks wrote — the closest thing to a re-render this
 * renderer-less suite has.
 */
function render(props: Parameters<typeof FeedVideo>[0]): Element {
  mockStateCursor = 0;
  return FeedVideo(props) as Element;
}

/** The visible video's tree. */
function visibleTree(): Element {
  return render({ uri: URI, isVisible: true });
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

beforeEach(() => {
  mockStateValues = [];
  mockStateCursor = 0;
});

describe('FeedVideo touch handling', () => {
  /**
   * The regression this guards (KNOT-020k-fix5): on Android the native video
   * surface sits above the React view it is drawn in, and it consumed the press
   * before it could reach the Pressable every caller wraps this component in. The
   * card's video then did nothing at all while an image in the same card opened
   * the viewer.
   */
  it('keeps the player out of the touch system so the caller receives the tap', () => {
    const anchor = elements(visibleTree()).find(
      (element) => element.props?.pointerEvents === 'none',
    );

    expect(anchor).toBeDefined();
    expect(anchor?.type).toBe('View');
    expect(firstOfType(anchor?.props?.children, 'Video')).toBeDefined();
  });

  it('leaves the mute control tappable above the player', () => {
    const mute = elements(visibleTree()).find(
      (element) => element.type === 'Pressable' && element.props?.accessibilityLabel !== undefined,
    );

    expect(mute).toBeDefined();
  });
});

describe('FeedVideo playback', () => {
  it('plays the visible video and loops it', () => {
    const video = firstOfType(visibleTree(), 'Video');
    const paused = video?.props?.paused;

    expect(paused).toBe(false);
    expect(video?.props?.repeat).toBe(true);
  });

  it('pauses the video that is off screen', () => {
    const tree = render({ uri: URI, isVisible: false });
    const video = firstOfType(tree, 'Video');

    expect(video?.props?.paused).toBe(true);
  });
});

describe('FeedVideo timestamp', () => {
  it('counts against the clip once the duration is known', () => {
    const tree = render({ uri: URI, isVisible: true, durationMs: 76_000 });

    expect(badgeText(tree)).toBe('0:00 / 1:16');
  });

  it('advances with the progress the player reports', () => {
    const before = render({ uri: URI, isVisible: true, durationMs: 76_000 });
    const progress = firstOfType(before, 'Video')?.props?.onProgress;

    expect(typeof progress).toBe('function');
    (progress as (data: { currentTime: number }) => void)({ currentTime: 15.4 });

    const after = render({ uri: URI, isVisible: true, durationMs: 76_000 });
    expect(badgeText(after)).toBe('0:15 / 1:16');
  });

  it('ignores a position that is not a real number', () => {
    render({ uri: URI, isVisible: true, durationMs: 76_000 });
    const progress = firstOfType(visibleTree(), 'Video')?.props?.onProgress;

    (progress as (data: { currentTime: number }) => void)({ currentTime: Number.NaN });

    const after = render({ uri: URI, isVisible: true, durationMs: 76_000 });
    expect(badgeText(after)).toBe('0:00 / 1:16');
  });

  it('does not subscribe to progress while the video is off screen', () => {
    const tree = render({ uri: URI, isVisible: false, durationMs: 76_000 });

    expect(firstOfType(tree, 'Video')?.props?.onProgress).toBeUndefined();
  });

  it('draws no badge before a duration is known', () => {
    const tree = render({ uri: URI, isVisible: true });

    expect(badgeText(tree)).toBe('');
  });
});

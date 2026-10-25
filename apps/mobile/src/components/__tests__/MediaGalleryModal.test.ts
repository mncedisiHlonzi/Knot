import MediaGalleryModal, { ViewerStage } from '../MediaGalleryModal';
import type { ViewerMedia, ViewerStory } from '../MediaGalleryModal';
import { spacing } from '../../theme';
import { lightTheme } from '../../theme/ThemeContext';
import { setNavigationBarColor, setStatusBarIcons } from '../../native/systemBars';
import { VIDEO_CONTROLS_HEIGHT } from '../VideoPlayer';

/**
 * The viewer stage owns several hook primitives, and this project tests
 * components by CALLING them directly (no renderer is installed). The
 * primitives are therefore stubbed: refs, state, memos and callbacks all run,
 * while effects are skipped — the chrome contract is pure render output, so
 * nothing they would do is needed here (KNOT-020k-fix6).
 */
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useEffect: () => undefined,
  useMemo: (factory: () => unknown) => factory(),
  useRef: (initial: unknown) => ({ current: initial }),
  useState: (initial: unknown) => [
    typeof initial === 'function' ? (initial as () => unknown)() : initial,
    () => undefined,
  ],
}));

jest.mock('react-native', () => {
  /** The smallest Animated.Value the stage reads: it interpolates and is set. */
  class Value {
    setValue(): void {
      return undefined;
    }

    interpolate(): number {
      return 0;
    }
  }

  return {
    Animated: {
      Value,
      View: 'Animated.View',
      spring: () => ({ start: () => undefined }),
      timing: () => ({ start: () => undefined }),
    },
    Easing: { bezier: () => undefined },
    FlatList: 'FlatList',
    Modal: 'Modal',
    PanResponder: { create: () => ({ panHandlers: {} }) },
    Platform: { OS: 'ios' },
    Pressable: 'Pressable',
    // A function component so the modal shell can build a `<StatusBar>` element
    // (an object as a JSX type throws), with the height the component reads at
    // module scope.
    StatusBar: (() => {
      const StatusBarStub = (): null => null;
      StatusBarStub.currentHeight = 24;
      return StatusBarStub;
    })(),
    StyleSheet: { absoluteFillObject: {}, create: (styles: unknown) => styles },
    Text: 'Text',
    View: 'View',
    useWindowDimensions: () => ({ height: 844, width: 390 }),
  };
});

jest.mock('phosphor-react-native', () => ({
  ArrowLeft: 'ArrowLeft',
}));

jest.mock('../Avatar', () => ({ __esModule: true, default: 'Avatar' }));

jest.mock('../VideoPlayer', () => ({
  __esModule: true,
  default: 'VideoPlayer',
  VIDEO_CONTROLS_HEIGHT: 88,
}));

jest.mock('../../native/systemBars', () => ({
  clearStatusBarIcons: jest.fn(),
  setNavigationBarColor: jest.fn(),
  setStatusBarIcons: jest.fn(),
}));

jest.mock('../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));

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

const MEDIA: readonly ViewerMedia[] = [
  { id: 'media-1', media_type: 'image', url: '/stories/story-1/media/media-1/content' },
];

const STORY: ViewerStory = {
  storyId: 'story-1',
  title: 'The story',
  body: 'A body long enough to quote a few lines of.',
  media: MEDIA,
  authorDisplayName: 'Nomsa Khan',
  authorAvatarUrl: null,
  createdAt: '2026-01-01T00:00:00Z',
};

type StageOverrides = Partial<Parameters<typeof ViewerStage>[0]>;

/** Calls the stage with a story the overlay can draw. */
function renderStage(overrides: StageOverrides = {}): {
  readonly tree: unknown;
  readonly onClose: jest.Mock;
} {
  const onClose = jest.fn();
  const tree = ViewerStage({
    media: MEDIA,
    initialIndex: 0,
    story: STORY,
    onClose,
    ...overrides,
  });

  return { tree, onClose };
}

/** A single video page, so the same stage can be asked how it draws one. */
const VIDEO_MEDIA: readonly ViewerMedia[] = [
  { id: 'media-v', media_type: 'video', url: '/stories/story-1/media/media-v/content' },
];

/** The story band's style layers, for a stage showing `media`. */
function bandStyles(
  media: readonly ViewerMedia[],
): ReadonlyArray<Record<string, unknown> | null | undefined> {
  const { tree } = renderStage({ media });
  const band = elements(tree).find((element) => {
    const style = element.props?.style;
    return (
      Array.isArray(style) &&
      (style[0] as { paddingHorizontal?: number } | undefined)?.paddingHorizontal !== undefined
    );
  });

  return (
    (band?.props?.style as Array<Record<string, unknown> | null | undefined> | undefined) ?? []
  );
}

/** The band's own style object, before any page-specific override. */
function bandStyle(media: readonly ViewerMedia[]): Record<string, unknown> | undefined {
  return bandStyles(media)[0] ?? undefined;
}

/**
 * The overlay's bottom inset after the page-specific layer is applied: a video page
 * adds the transport's height, an image page keeps the base inset.
 */
function bandPadding(media: readonly ViewerMedia[]): number {
  const layers = bandStyles(media);
  const value = layers[1]?.paddingBottom ?? layers[0]?.paddingBottom;
  return typeof value === 'number' ? value : 0;
}

describe('MediaGalleryModal status bar', () => {
  it('declares white icons over a transparent bar for the black room', () => {
    const tree = MediaGalleryModal({
      visible: true,
      media: MEDIA,
      initialIndex: 0,
      story: STORY,
      onClose: () => undefined,
    });
    const statusBar = elements(tree).find((element) => element.props?.barStyle !== undefined);

    expect(statusBar?.props?.barStyle).toBe('light-content');
    expect(statusBar?.props?.backgroundColor).toBe('transparent');
    expect(statusBar?.props?.translucent).toBe(true);
  });
});

describe('MediaGalleryModal system chrome', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  /**
   * The regression this guards (KNOT-020k-fix15): the viewer's paint used to run
   * only from a passive effect that fires in the commit which *builds* the dialog,
   * and it only forced the status-bar icons — never the bar's fill. The dialog's
   * own window is the one that paints the bars, so the paint is repeated when the
   * dialog reports it is shown, and it carries the theatre black.
   */
  it('paints both system bars black when the dialog reports it is shown', () => {
    const tree = MediaGalleryModal({
      visible: true,
      media: MEDIA,
      initialIndex: 0,
      story: STORY,
      onClose: () => undefined,
    });
    const modal = elements(tree).find((element) => element.type === 'Modal');
    const onShow = modal?.props?.onShow;

    expect(typeof onShow).toBe('function');
    (onShow as () => void)();

    expect(setNavigationBarColor as jest.Mock).toHaveBeenCalledWith(
      lightTheme.colors.bg.theater,
      false,
    );
    expect(setStatusBarIcons as jest.Mock).toHaveBeenCalledWith(true, lightTheme.colors.bg.theater);
  });
});

describe('MediaGalleryModal header navigation', () => {
  it('draws the close control on the left with a back arrow', () => {
    const { tree } = renderStage();

    const topBar = elements(tree).find((element) => {
      const style = element.props?.style;
      return (
        Array.isArray(style) &&
        (style[0] as { position?: string } | undefined)?.position === 'absolute' &&
        (style[0] as { flexDirection?: string } | undefined)?.flexDirection === 'row'
      );
    });
    const children = (topBar?.props?.children ?? []) as Element[];

    expect(children[0]?.props?.accessibilityLabel).toBe('Close media viewer');
    expect(elements(children[0]).some((element) => element.type === 'ArrowLeft')).toBe(true);
    expect(elements(tree).some((element) => element.type === 'X')).toBe(false);
  });

  it("keeps the counter as the top bar's right-hand slot", () => {
    const { tree } = renderStage({
      media: [MEDIA[0], { ...MEDIA[0], id: 'media-2' }],
      story: { ...STORY, media: MEDIA },
    });
    const counter = elements(tree).find((element) => element.props?.children === '1 / 2');

    expect(counter).toBeDefined();
  });
});

describe('MediaGalleryModal story overlay', () => {
  it('draws no surface behind the story text on an image page', () => {
    expect(bandStyle(MEDIA)?.backgroundColor).toBeUndefined();
  });

  it('draws no surface behind the story text on a video page', () => {
    expect(bandStyle(VIDEO_MEDIA)?.backgroundColor).toBeUndefined();
  });

  it('hugs the bottom edge on an image page, with no room for controls', () => {
    expect(bandStyle(MEDIA)?.bottom).toBe(0);
    expect(bandPadding(MEDIA)).toBe(spacing.lg);
  });

  it('reserves the transport height on a video page only', () => {
    expect(bandPadding(VIDEO_MEDIA)).toBe(VIDEO_CONTROLS_HEIGHT + spacing.lg);
    expect(bandPadding(VIDEO_MEDIA) - bandPadding(MEDIA)).toBe(VIDEO_CONTROLS_HEIGHT);
  });

  it('keeps the story text legible with its media shadow', () => {
    const { tree } = renderStage();
    const title = elements(tree).find((element) => element.props?.children === STORY.title);
    const titleStyle = title?.props?.style as Array<Record<string, unknown>> | undefined;

    expect(titleStyle?.[1]).toMatchObject({ textShadowRadius: 5 });
  });

  it('draws neither of the removed action icons', () => {
    const { tree } = renderStage();
    const labels = elements(tree).map((element) => element.props?.accessibilityLabel);

    expect(elements(tree).some((element) => element.type === 'ChatCircle')).toBe(false);
    expect(elements(tree).some((element) => element.type === 'TreeStructure')).toBe(false);
    expect(labels).not.toContain('Open the conversation');
    expect(labels).not.toContain('Open the language tree');
  });
});

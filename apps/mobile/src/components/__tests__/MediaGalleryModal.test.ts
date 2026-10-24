import { ViewerStage } from '../MediaGalleryModal';
import type { ViewerMedia, ViewerStory } from '../MediaGalleryModal';

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
    StatusBar: { currentHeight: 24 },
    StyleSheet: { absoluteFillObject: {}, create: (styles: unknown) => styles },
    Text: 'Text',
    View: 'View',
    useWindowDimensions: () => ({ height: 844, width: 390 }),
  };
});

jest.mock('phosphor-react-native', () => ({
  ArrowLeft: 'ArrowLeft',
  ChatCircle: 'ChatCircle',
  TreeStructure: 'TreeStructure',
}));

jest.mock('../Avatar', () => ({ __esModule: true, default: 'Avatar' }));

jest.mock('../VideoPlayer', () => ({
  __esModule: true,
  default: 'VideoPlayer',
  VIDEO_CONTROLS_HEIGHT: 88,
}));

jest.mock('../../native/systemBars', () => ({ setNavigationBarColor: jest.fn() }));

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

/** The one element carrying an accessibility label, or undefined. */
function byLabel(tree: unknown, label: string): Element | undefined {
  return elements(tree).find((element) => element.props?.accessibilityLabel === label);
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

/** Calls the stage with a story and spies for the two new actions. */
function renderStage(overrides: StageOverrides = {}): {
  readonly tree: unknown;
  readonly onClose: jest.Mock;
  readonly onOpenComments: jest.Mock;
  readonly onOpenTree: jest.Mock;
} {
  const onClose = jest.fn();
  const onOpenComments = jest.fn();
  const onOpenTree = jest.fn();
  const tree = ViewerStage({
    media: MEDIA,
    initialIndex: 0,
    story: STORY,
    onClose,
    onOpenComments,
    onOpenTree,
    ...overrides,
  });

  return { tree, onClose, onOpenComments, onOpenTree };
}

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

describe('MediaGalleryModal story actions', () => {
  it('shows the comment and language-tree glyphs', () => {
    const { tree } = renderStage();

    expect(elements(tree).some((element) => element.type === 'ChatCircle')).toBe(true);
    expect(elements(tree).some((element) => element.type === 'TreeStructure')).toBe(true);
  });

  it('gives each action a 44pt tap target', () => {
    const { tree } = renderStage();
    const comment = byLabel(tree, 'Open the conversation');
    const treeAction = byLabel(tree, 'Open the language tree');

    expect(comment?.props?.style).toMatchObject({ height: 44, width: 44 });
    expect(treeAction?.props?.style).toMatchObject({ height: 44, width: 44 });
  });

  it('opens the conversation for the current story and closes the viewer', () => {
    const { tree, onClose, onOpenComments, onOpenTree } = renderStage();
    const comment = byLabel(tree, 'Open the conversation');

    (comment?.props?.onPress as () => void)();

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onOpenComments).toHaveBeenCalledWith('story-1');
    expect(onOpenTree).not.toHaveBeenCalled();
  });

  it('opens the language tree for the current story and closes the viewer', () => {
    const { tree, onClose, onOpenComments, onOpenTree } = renderStage();
    const treeAction = byLabel(tree, 'Open the language tree');

    (treeAction?.props?.onPress as () => void)();

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onOpenTree).toHaveBeenCalledWith('story-1');
    expect(onOpenComments).not.toHaveBeenCalled();
  });

  it('draws no story chrome at all for a pure-theatre caller', () => {
    const { tree } = renderStage({ story: null });

    expect(byLabel(tree, 'Open the conversation')).toBeUndefined();
    expect(byLabel(tree, 'Open the language tree')).toBeUndefined();
  });

  it('draws only the action whose handler the caller provided', () => {
    const { tree } = renderStage({ onOpenComments: undefined });

    expect(byLabel(tree, 'Open the conversation')).toBeUndefined();
    expect(byLabel(tree, 'Open the language tree')).toBeDefined();
  });
});

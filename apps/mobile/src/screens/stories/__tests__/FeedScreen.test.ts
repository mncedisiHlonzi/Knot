import type { FeedItem } from '../../../api/stories';
import Avatar from '../../../components/Avatar';
import ConfirmSheet from '../../../components/ConfirmSheet';
import FeedEntry from '../../../components/FeedEntry';
import Header from '../../../components/Header';
import { colors } from '../../../theme';
import FeedScreen from '../FeedScreen';
import { PROMPTS, nextPromptIndex, promptAt, promptIndexForPillar } from '../feedPrompt';

/**
 * `FeedScreen` owns the feed's layout and the two hide entry points. This project
 * has no renderer and must not add one, so the screen is called as a plain
 * function with React's hook primitives stubbed: `useCallback` returns its
 * callback unchanged, `useEffect` never runs (so nothing is fetched), `useRef`
 * hands back a mutable box, and `useState` returns the initial value while
 * recording the setter so a handler's state writes can be inspected. Every setter
 * is collected, so no test has to know which state slot is which (KNOT-032-fix1).
 */
const mockStateSetters: jest.Mock[] = [];

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useEffect: () => undefined,
  useRef: (value: unknown) => ({ current: value }),
  useState: (initial: unknown) => {
    const setter = jest.fn();
    mockStateSetters.push(setter);
    return [initial, setter];
  },
}));

jest.mock('react-native', () => ({
  ActivityIndicator: 'ActivityIndicator',
  // The prompt card draws the shared Button and Chip, which build their animated
  // wrappers at module scope (KNOT-020k-fix7).
  Animated: {
    Value: class {
      setValue(): void {}
      interpolate(): number {
        return 0;
      }
    },
    View: 'Animated.View',
    createAnimatedComponent: <T>(component: T): T => component,
    loop: () => ({ start: () => undefined, stop: () => undefined }),
    spring: () => ({ start: () => undefined, stop: () => undefined }),
    timing: () => ({ start: () => undefined, stop: () => undefined }),
  },
  Easing: { bezier: () => (progress: number) => progress },
  FlatList: 'FlatList',
  Modal: 'Modal',
  Pressable: 'Pressable',
  RefreshControl: 'RefreshControl',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
  useWindowDimensions: () => ({ width: 390, height: 844 }),
}));

jest.mock('phosphor-react-native', () => ({
  ArrowsClockwise: () => null,
  Bell: () => null,
  DotsThree: () => null,
  EyeSlash: () => null,
  Flag: () => null,
  Images: () => null,
  MagnifyingGlass: () => null,
  PencilSimple: () => null,
  Plus: () => null,
  ShareNetwork: () => null,
  X: () => null,
}));

jest.mock('react-native-video', () => ({ __esModule: true, default: 'Video' }));
jest.mock('../../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));
jest.mock('../../../api/client', () => ({ describeError: () => 'request failed' }));
jest.mock('../../../api/notifications', () => ({
  notificationsApi: { getUnreadCount: jest.fn() },
}));
jest.mock('../../../api/stories', () => ({
  FEED_PAGE_SIZE: 20,
  storiesApi: { listStories: jest.fn() },
}));

type ElementLike = { type?: unknown; props?: Record<string, unknown> };

/** Finds the first element of the given type in a directly-called component's tree. */
function findElementByType(node: unknown, type: unknown): ElementLike | undefined {
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

  const element = node as ElementLike;
  if (element.type === type) {
    return element;
  }

  return findElementByType(element.props?.children, type);
}

/**
 * Finds the first element of the given type anywhere under `node`, walking the
 * element's props as well as its children. The Feed's header is an element whose
 * leading and trailing nodes live in `left`/`right` props, not in `children`, so
 * a children-only walk cannot see the avatar in the bar.
 */
function findInProps(node: unknown, type: unknown): ElementLike | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findInProps(child, type);
      if (found !== undefined) {
        return found;
      }
    }
    return undefined;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return undefined;
  }

  const element = node as ElementLike;
  if (element.type === type) {
    return element;
  }

  for (const value of Object.values(element.props ?? {})) {
    const found = findInProps(value, type);
    if (found !== undefined) {
      return found;
    }
  }
  return undefined;
}

/** Finds the first element carrying the given testID. */
function findByTestID(node: unknown, testID: string): ElementLike | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findByTestID(child, testID);
      if (found !== undefined) {
        return found;
      }
    }
    return undefined;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return undefined;
  }

  const element = node as ElementLike;
  if (element.props?.testID === testID) {
    return element;
  }

  return findByTestID(element.props?.children, testID);
}

/** The entry the feed draws in every assertion below. */ const ITEM: FeedItem = {
  id: 'story-1',
  kind: 'story',
  author_id: 'user-1',
  author_display_name: 'Nomsa Khan',
  root_version_id: 'version-1',
  pillar: 'heritage',
  language: 'zap',
  title: 'How my grandmother counted the days',
  body: 'She kept a string of knots.',
  approximate_location: 'Oaxaca, Mexico',
  media_urls: [],
  created_at: '2026-10-09T12:00:00.000Z',
  updated_at: '2026-10-09T12:00:00.000Z',
};

const SCREEN_PROPS = {
  token: 'token',
  viewer: { display_name: 'Nomsa Khan', avatar_url: '' },
  onOpenStory: () => undefined,
  onOpenVersion: () => undefined,
  onOpenUserProfile: () => undefined,
  onOpenProfile: () => undefined,
  onOpenMedia: () => undefined,
  onOpenSearch: () => undefined,
  onCreateStory: () => undefined,
};

/** One render of the screen, with the state setters reset first. */
function renderScreen(): unknown {
  mockStateSetters.length = 0;
  return FeedScreen(SCREEN_PROPS);
}

/** The card the list builds for one entry: its real `renderItem`, called directly. */
function cardFor(tree: unknown): ElementLike | undefined {
  const list = findElementByType(tree, 'FlatList');
  const renderItem = list?.props?.renderItem as
    ((info: { item: FeedItem }) => ElementLike) | undefined;

  return renderItem?.({ item: ITEM });
}

describe('FeedScreen card structure', () => {
  it('draws each entry as a full-width white card, with no side gutter', () => {
    const card = cardFor(renderScreen());
    const style = card?.props?.style as Record<string, unknown> | undefined;

    expect(style?.backgroundColor).toBe(colors.bg.canvas);
    // Full width: no horizontal inset, so no grey gutter runs down the sides.
    expect(style?.marginHorizontal).toBeUndefined();
  });

  // The separator is a band again, not a hairline: 4pt is noticeably thicker than a
  // rule and nothing like the old 8pt gutter (KNOT-020k-fix20).
  it('closes each card with a 4pt band of the separator neutral', () => {
    const card = cardFor(renderScreen());
    const style = card?.props?.style as Record<string, unknown> | undefined;

    expect(style?.borderBottomWidth).toBe(4);
    expect(style?.borderBottomColor).toBe(colors.bg.separator);
    // Drawn as the card's own border, so there is no gap between two cards.
    expect(style?.marginBottom).toBeUndefined();
    // The separator neutral IS the hairline neutral, so the feed's bands and the
    // rest of the chrome's hairlines can never drift apart.
    expect(colors.bg.separator).toBe(colors.border.hairline);
  });

  it('paints the page with the canvas, so the rules do the separating', () => {
    const container = findElementByType(renderScreen(), 'View');
    const style = container?.props?.style as Record<string, unknown> | undefined;

    expect(style?.backgroundColor).toBe(colors.bg.canvas);
  });

  it('leaves the entry rule off, so the card rule is the only separation', () => {
    const entry = findElementByType(cardFor(renderScreen()), FeedEntry);

    expect(entry?.props?.showBottomHairline).toBe(false);
  });

  // The tab row and the first story are parted by the same 4pt band that closes a
  // card, so the rhythm stays even (KNOT-020j-fix2, KNOT-020k-fix20).
  it('parts the tab row from the first story with the same 4pt band', () => {
    const divider = findByTestID(renderScreen(), 'feed-tabs-divider');
    const style = divider?.props?.style as Record<string, unknown> | undefined;

    expect(divider).toBeDefined();
    expect(style?.backgroundColor).toBe(colors.bg.separator);
    expect(style?.height).toBe(4);
  });
});

// Create moved out of the tab bar and into the feed itself (KNOT-020k-fix6), so
// the composer is the list's first row: a face, a prompt, and the brand disc that
// opens the composer screen.
// The composer row is gone (KNOT-020k-fix7). In its place the feed heads the list
// with a writing prompt: a family overline, the question, a primary "Write it"
// action and the two pillar chips.
describe('FeedScreen prompt card', () => {
  /** The PromptCard element in the list's header component (not yet called). */
  const cardElementFor = (tree: unknown): ElementLike | undefined => {
    const list = findElementByType(tree, 'FlatList');
    return findElement(list?.props?.ListHeaderComponent, (props) => {
      return typeof props.onShuffle === 'function' && typeof props.onWrite === 'function';
    });
  };

  /** The card's own tree, by calling the component the list header holds. */
  const cardFor = (tree: unknown): ElementLike | undefined => {
    const element = cardElementFor(tree);
    if (element === undefined) {
      return undefined;
    }
    return (element.type as (props: unknown) => ElementLike)(element.props);
  };

  /** The single state updater a tap produced, so its effect can be applied. */
  const lastUpdater = (): ((index: number) => number) | undefined => {
    const writes = mockStateSetters.flatMap((setter) => setter.mock.calls);
    const updaters = writes
      .map(([value]) => value)
      .filter((value): value is (index: number) => number => typeof value === 'function');

    return updaters[updaters.length - 1];
  };

  it('heads the list with a prompt from the compiled list', () => {
    const element = cardElementFor(renderScreen());

    expect(element).toBeDefined();
    expect(element?.props?.prompt).toBe(PROMPTS[0]);
  });

  it('draws the family overline, the question, the action and both pillar chips', () => {
    const card = cardFor(renderScreen());

    expect(findByTestID(card, 'feed-prompt')).toBeDefined();
    expect(findByTestID(card, 'feed-prompt-text')).toBeDefined();
    expect(findByTestID(card, 'feed-prompt-shuffle')).toBeDefined();

    // The overline names the kind of story the prompt asks for (KNOT-020k-fix8).
    expect(findElement(card, (props) => props.children === 'Wonder story')).toBeDefined();
    expect(findElement(card, (props) => props.children === 'Wonder prompt')).toBeUndefined();

    const write = findByTestID(card, 'feed-prompt-write');
    expect(write?.props?.label).toBe('Write it');

    // The chips name both pillars, and the one the prompt belongs to is selected.
    expect(findByTestID(card, 'feed-prompt-chip-wonder')?.props?.label).toBe('Wonder');
    expect(findByTestID(card, 'feed-prompt-chip-heritage')?.props?.label).toBe('Heritage');
    expect(findByTestID(card, 'feed-prompt-chip-wonder')?.props?.selected).toBe(true);
    expect(findByTestID(card, 'feed-prompt-chip-heritage')?.props?.selected).toBe(false);

    // Heavier than a filter chip, and the chosen one is a solid surface while the
    // other keeps only its hairline (KNOT-020k-fix8).
    expect(findByTestID(card, 'feed-prompt-chip-wonder')?.props?.textScale).toBe('button');
    expect(findByTestID(card, 'feed-prompt-chip-wonder')?.props?.variant).toBe('default');
    expect(findByTestID(card, 'feed-prompt-chip-heritage')?.props?.variant).toBe('outline');
  });

  // Both the button and the question open the composer: the button is the labelled
  // affordance, the question the larger target.
  it('opens the story composer from the button and from the question', () => {
    const onCreateStory = jest.fn();
    const card = cardFor(FeedScreen({ ...SCREEN_PROPS, onCreateStory }));

    (findByTestID(card, 'feed-prompt-write')?.props?.onPress as () => void)();
    (findByTestID(card, 'feed-prompt-text')?.props?.onPress as () => void)();

    expect(onCreateStory).toHaveBeenCalledTimes(2);
  });

  it('deals the next prompt when the shuffle is tapped', () => {
    const card = cardFor(renderScreen());

    (findByTestID(card, 'feed-prompt-shuffle')?.props?.onPress as () => void)();

    const updater = lastUpdater();
    expect(updater).toBeDefined();
    expect(updater?.(0)).toBe(nextPromptIndex(0));
    // Never the prompt already on screen.
    expect(updater?.(0)).not.toBe(0);
  });

  it('deals the next prompt of the chosen pillar when a chip is tapped', () => {
    const card = cardFor(renderScreen());

    (findByTestID(card, 'feed-prompt-chip-heritage')?.props?.onPress as () => void)();

    const next = lastUpdater()?.(0);
    expect(next).toBe(promptIndexForPillar(0, 'heritage'));
    expect(promptAt(next ?? 0).pillar).toBe('heritage');
  });

  // The row that fix6 added is gone, not merely hidden.
  it('no longer draws the old composer row', () => {
    const list = findElementByType(renderScreen(), 'FlatList');

    expect(findByTestID(list?.props?.ListHeaderComponent, 'feed-composer')).toBeUndefined();
    expect(
      findElement(
        list?.props?.ListHeaderComponent,
        (props) => props.accessibilityLabel === 'Create a story',
      ),
    ).toBeUndefined();
  });
});

describe('FeedScreen header', () => {
  it('draws the signed-in person as an avatar beside search', () => {
    const header = findElementByType(renderScreen(), Header);
    const avatar = findInProps(header?.props?.right, Avatar);

    expect(avatar).toBeDefined();
    expect(avatar?.props?.displayName).toBe('Nomsa Khan');
    // No uploaded avatar in this fixture, so the initials fallback is what draws.
    expect(avatar?.props?.uri).toBeUndefined();
    // The avatar is the tappable route to the signed-in person's profile
    // (KNOT-020j): the bell it replaced has moved to the tab bar.
    expect(typeof avatar?.props?.onPress).toBe('function');
  });

  // Create moved from the composer row into the header's action pair
  // (KNOT-020k-fix8), so writing is reachable from anywhere in the feed.
  it('draws the create control in the header, beside search', () => {
    const header = findElementByType(renderScreen(), Header);
    const create = findElement(
      header?.props?.right,
      (props) => props.accessibilityLabel === 'Create a story',
    );

    expect(create).toBeDefined();
    expect(typeof create?.props?.onPress).toBe('function');
    // A full 44pt target, like the search control it sits beside.
    expect(create?.props?.style).toMatchObject({ height: 44, width: 44 });
  });

  it('opens the composer from the header create control', () => {
    const onCreateStory = jest.fn();
    const header = findElementByType(FeedScreen({ ...SCREEN_PROPS, onCreateStory }), Header);
    const create = findElement(
      header?.props?.right,
      (props) => props.accessibilityLabel === 'Create a story',
    );

    (create?.props?.onPress as () => void)();

    expect(onCreateStory).toHaveBeenCalledTimes(1);
  });
});

describe('FeedScreen hide confirmation', () => {
  it('opens with nothing pending and the confirmation copy the feed needs', () => {
    const sheet = findElementByType(renderScreen(), ConfirmSheet);

    expect(sheet).toBeDefined();
    // Nothing has asked to hide anything yet.
    expect(sheet?.props?.visible).toBe(false);
    expect(sheet?.props?.title).toBe('Hide this story?');
    expect(sheet?.props?.confirmLabel).toBe('Hide story');
    expect(sheet?.props?.cancelLabel).toBe('Keep story');
  });

  it('records a pending hide instead of removing the entry from the card', () => {
    const tree = renderScreen();
    const entry = findElementByType(cardFor(tree), FeedEntry);

    expect(entry?.props?.onHide).toBeDefined();
    (entry?.props?.onHide as () => void)();

    const writes = mockStateSetters.flatMap((setter) => setter.mock.calls as unknown[][]);
    // The request names the entry…
    expect(writes.some(([value]) => value === ITEM.id)).toBe(true);
    // …and never runs the list's filter updater, which is the only thing that
    // removes an entry. Hiding happens on confirmation, not on the tap.
    expect(writes.every(([value]) => typeof value !== 'function')).toBe(true);
  });
});

/** Finds the first element whose props satisfy the predicate. */
function findElement(
  node: unknown,
  predicate: (props: Record<string, unknown>) => boolean,
): ElementLike | undefined {
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

  const element = node as ElementLike;
  if (element.props !== undefined && predicate(element.props)) {
    return element;
  }
  return findElement(element.props?.children, predicate);
}

describe('FeedScreen ordering tabs', () => {
  it('offers For you and Latest beside the visual-only filters', () => {
    const tree = renderScreen();

    for (const label of ['For you', 'Latest', 'Wonder', 'Heritage', 'Nearby']) {
      const tab = findElement(
        tree,
        (props) => props.accessibilityLabel === label && typeof props.onPress === 'function',
      );
      expect(tab).toBeDefined();
    }
  });

  it('records the ordering tab a person taps, so the feed can re-fetch', () => {
    // The re-fetch itself lives in `loadFirstPage`, which depends on the derived
    // feed type; this suite stubs `useEffect`, so the tab's state write is the
    // observable half (KNOT-027).
    const tree = renderScreen();
    const latest = findElement(
      tree,
      (props) => props.accessibilityLabel === 'Latest' && typeof props.onPress === 'function',
    );

    (latest?.props?.onPress as () => void)();

    const writes = mockStateSetters.flatMap((setter) => setter.mock.calls as unknown[][]);
    expect(writes.some(([value]) => value === 'Latest')).toBe(true);
  });
});

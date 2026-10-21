import type { FeedItem } from '../../../api/stories';
import ConfirmSheet from '../../../components/ConfirmSheet';
import FeedEntry from '../../../components/FeedEntry';
import { colors, spacing } from '../../../theme';
import FeedScreen from '../FeedScreen';

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
  Bell: () => null,
  DotsThree: () => null,
  EyeSlash: () => null,
  Flag: () => null,
  Images: () => null,
  MagnifyingGlass: () => null,
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

/** The entry the feed draws in every assertion below. */
const ITEM: FeedItem = {
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
  onOpenStory: () => undefined,
  onOpenVersion: () => undefined,
  onOpenUserProfile: () => undefined,
  onOpenNotifications: () => undefined,
  onOpenSearch: () => undefined,
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
    // No rule on the card: the band between cards does the separating.
    expect(style?.borderBottomWidth).toBeUndefined();
    expect(style?.borderBottomColor).toBeUndefined();
  });

  it('holds the cards apart with one compact band of the separator surface', () => {
    const card = cardFor(renderScreen());
    const style = card?.props?.style as Record<string, unknown> | undefined;

    // 8pt is the design system's compact step, inside the 6–8pt target: enough
    // grey to separate two stories, not enough to open a gap.
    expect(style?.marginBottom).toBe(spacing.sm);
    expect(spacing.sm).toBeGreaterThanOrEqual(6);
    expect(spacing.sm).toBeLessThanOrEqual(8);
  });

  it('paints the page with the separator surface, darker than the old grey', () => {
    const container = findElementByType(renderScreen(), 'View');
    const style = container?.props?.style as Record<string, unknown> | undefined;

    expect(style?.backgroundColor).toBe(colors.bg.separator);
    // `bg.sunken` was too close to white to read at 8pt, and the canvas is white.
    expect(colors.bg.separator).not.toBe(colors.bg.sunken);
    expect(colors.bg.separator).not.toBe(colors.bg.canvas);
  });

  it('leaves the entry rule off, so the band is the only separation', () => {
    const entry = findElementByType(cardFor(renderScreen()), FeedEntry);

    expect(entry?.props?.showBottomHairline).toBe(false);
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

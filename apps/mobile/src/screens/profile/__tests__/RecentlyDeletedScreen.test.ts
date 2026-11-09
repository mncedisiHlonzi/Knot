import { Alert } from 'react-native';

import type { Story } from '../../../api/stories';
import { storiesApi } from '../../../api/stories';
import EmptyState from '../../../components/EmptyState';
import RecentlyDeletedScreen from '../RecentlyDeletedScreen';

/**
 * Recently Deleted is the author's recycle bin: it lists the caller's own
 * soft-deleted stories and offers Restore or Delete Forever (KNOT-054).
 *
 * The project has no renderer, so the screen is called as a plain function with
 * React's hook primitives stubbed, the pattern the other stateful screens use.
 * `useEffect` never runs (the scripted state IS the loaded state), `useCallback`
 * returns its callback unchanged, and `useState` is fed a positional sequence
 * while recording its setters.
 *
 * The sequence is: stories, loading, error, busyId.
 */
const mockStateValues: unknown[] = [];
const mockStateSetters: jest.Mock[] = [];
let mockStateCursor = 0;

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useEffect: () => undefined,
  useRef: (initial: unknown) => ({ current: initial }),
  useState: (initial: unknown) => {
    const value = mockStateValues[mockStateCursor];
    mockStateCursor += 1;
    const setter = jest.fn();
    mockStateSetters.push(setter);
    return [value === undefined ? initial : value, setter];
  },
}));

jest.mock('react-native', () => ({
  ActivityIndicator: 'ActivityIndicator',
  Alert: { alert: jest.fn() },
  Animated: { createAnimatedComponent: (component: unknown) => component },
  Easing: { bezier: () => () => 0 },
  FlatList: 'FlatList',
  Image: 'Image',
  Pressable: 'Pressable',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
  useWindowDimensions: () => ({ height: 844, width: 390 }),
}));

jest.mock('phosphor-react-native', () => ({
  ArrowLeft: () => null,
  Trash: () => null,
}));

// `FeedVideo` is not drawn here, but `Button`'s press animation module still
// imports the theme and motion primitives at module scope.
jest.mock('../../../api/client', () => ({ describeError: () => 'request failed' }));
jest.mock('../../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));
jest.mock('../../../api/stories', () => ({
  storiesApi: {
    listDeletedStories: jest.fn(),
    restoreStory: jest.fn(),
    permanentlyDeleteStory: jest.fn(),
  },
}));

type ElementLike = { type?: unknown; props?: Record<string, unknown> };

/** Finds the first element of the given type anywhere in the tree. */
function findByType(node: unknown, type: unknown): ElementLike | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findByType(child, type);
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

  return findByType(element.props?.children, type);
}

/** Finds the first element carrying the given testID (props included). */
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

  for (const value of Object.values(element.props ?? {})) {
    const found = findByTestID(value, testID);
    if (found !== undefined) {
      return found;
    }
  }
  return undefined;
}

/** One render with the scripted state, exactly as the app builds it. */
function renderScreen(values: readonly unknown[]): unknown {
  mockStateValues.length = 0;
  mockStateValues.push(...values);
  mockStateCursor = 0;
  mockStateSetters.length = 0;

  return RecentlyDeletedScreen({ token: 'token', onBack: () => undefined });
}

/** The row the list builds for one deleted story. */
function rowFor(tree: unknown, story: Story): ElementLike {
  const list = findByType(tree, 'FlatList');
  const renderItem = list?.props?.renderItem as
    ((info: { item: Story; index: number }) => ElementLike) | undefined;

  return renderItem?.({ item: story, index: 0 }) as ElementLike;
}

/** A deleted story with a thumbnail, as the API carries it. */
const STORY: Story = {
  id: 'story-1',
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
  first_media_type: 'image',
  first_media_url: '/stories/story-1/media/m1/content?variant=thumb',
  first_media_thumbnail_url: '/stories/story-1/media/m1/content?variant=thumb',
};

/** Every state updater the screen has written, in order. */
function updaters(): ((stories: readonly Story[]) => readonly Story[])[] {
  return mockStateSetters
    .flatMap((setter) => setter.mock.calls as unknown[][])
    .map(([value]) => value)
    .filter(
      (value): value is (stories: readonly Story[]) => readonly Story[] =>
        typeof value === 'function',
    );
}

describe('RecentlyDeletedScreen', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('lists each deleted story with its title and both actions', () => {
    const tree = renderScreen([[STORY], false, undefined, undefined]);
    const row = rowFor(tree, STORY);

    expect(findByTestID(row, 'restore-story-1')?.props?.label).toBe('Restore');
    expect(findByTestID(row, 'purge-story-1')?.props?.label).toBe('Delete forever');
    // The still the author uploaded is drawn from the thumbnail.
    expect(findByType(row, 'Image')?.props?.source).toEqual({
      uri: 'http://localhost:8080/stories/story-1/media/m1/content?variant=thumb',
    });
  });

  it('restores a story and removes it from the bin', async () => {
    (storiesApi.restoreStory as jest.Mock).mockResolvedValue({ story: STORY });
    const tree = renderScreen([[STORY], false, undefined, undefined]);

    (findByTestID(rowFor(tree, STORY), 'restore-story-1')?.props?.onPress as () => void)();
    await Promise.resolve();

    expect(storiesApi.restoreStory).toHaveBeenCalledWith('story-1', 'token');
    const [removal] = updaters();
    expect(removal([STORY])).toEqual([]);
  });

  it('asks before a permanent removal, and names that it cannot be undone', () => {
    const tree = renderScreen([[STORY], false, undefined, undefined]);

    (findByTestID(rowFor(tree, STORY), 'purge-story-1')?.props?.onPress as () => void)();

    expect(Alert.alert).toHaveBeenCalledTimes(1);
    const [title, message, buttons] = (Alert.alert as jest.Mock).mock.calls[0] as [
      string,
      string,
      { text: string }[],
    ];
    expect(title).toBe('Delete this story forever?');
    expect(message).toContain('cannot be undone');
    expect(buttons.map((button) => button.text)).toEqual(['Cancel', 'Delete forever']);
    expect(storiesApi.permanentlyDeleteStory).not.toHaveBeenCalled();
  });

  it('removes the story for good when the confirmation is accepted', async () => {
    (storiesApi.permanentlyDeleteStory as jest.Mock).mockResolvedValue(undefined);
    const tree = renderScreen([[STORY], false, undefined, undefined]);

    (findByTestID(rowFor(tree, STORY), 'purge-story-1')?.props?.onPress as () => void)();
    const buttons = (Alert.alert as jest.Mock).mock.calls[0]?.[2] as
      { text: string; onPress?: () => void }[] | undefined;
    buttons?.find((button) => button.text === 'Delete forever')?.onPress?.();
    await Promise.resolve();

    expect(storiesApi.permanentlyDeleteStory).toHaveBeenCalledWith('story-1', 'token');
    const [removal] = updaters();
    expect(removal([STORY])).toEqual([]);
  });

  it('draws the empty state when the bin holds nothing', () => {
    const tree = renderScreen([[], false, undefined, undefined]);
    const list = findByType(tree, 'FlatList');

    expect(findByType(list?.props?.ListEmptyComponent, EmptyState)).toBeDefined();
  });
});

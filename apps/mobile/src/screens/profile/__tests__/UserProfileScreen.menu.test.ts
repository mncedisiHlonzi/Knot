import { Alert } from 'react-native';

import type { Activity, ProfileUser } from '../../../api/profile';
import { storiesApi } from '../../../api/stories';
import UserProfileScreen from '../UserProfileScreen';

/**
 * The owner's story cards carry an overflow control that offers Edit and Delete
 * (KNOT-054); a version card carries none, and a visitor sees no card menu at all.
 *
 * The project has no renderer, so the screen is called as a plain function with
 * React's hook primitives stubbed, the pattern the wall's other suite uses.
 * `useEffect` never runs (the scripted state IS the loaded wall), `useCallback`
 * returns its callback unchanged, and `useState` is fed a positional sequence.
 *
 * The sequence in hook order is: profile 0, activities 1, nextCursor 2,
 * loading 3, refreshing 4, loadingMore 5, error 6, activeTab 7, activeItemId 8,
 * isMuted 9, blocking 10, blockError 11, visitorMenuOpen 12, **cardMenu 13**,
 * cardBusyId 14.
 */
const mockKeepInitial = Symbol('keep initial value');
const mockStateValues: unknown[] = [];
const mockStateSetters: jest.Mock[] = [];

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useEffect: () => undefined,
  useRef: (initial: unknown) => ({ current: initial }),
  useState: (initial: unknown) => {
    const setter = jest.fn();
    mockStateSetters.push(setter);
    const scripted = mockStateValues.length > 0 ? mockStateValues.shift() : mockKeepInitial;
    return [scripted === mockKeepInitial ? initial : scripted, setter];
  },
}));

jest.mock('react-native', () => ({
  ActivityIndicator: 'ActivityIndicator',
  Alert: { alert: jest.fn() },
  Animated: {
    Value: class {
      setValue(): void {}
      interpolate(): number {
        return 0;
      }
    },
    View: 'Animated.View',
  },
  FlatList: 'FlatList',
  Image: 'Image',
  Modal: 'Modal',
  Pressable: 'Pressable',
  RefreshControl: 'RefreshControl',
  ScrollView: 'ScrollView',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

jest.mock('phosphor-react-native', () => ({
  ArrowLeft: () => null,
  DotsThree: () => null,
  Gear: () => null,
}));

jest.mock('react-native-video', () => ({ __esModule: true, default: 'Video' }));
jest.mock('../../../motion/useFade', () => ({
  useFade: () => ({ opacity: 0, fadeIn: () => undefined, fadeOut: () => undefined }),
}));

jest.mock('../../../api/client', () => ({ describeError: () => 'request failed' }));
jest.mock('../../../api/moderation', () => ({
  blocksApi: { block: jest.fn(), unblock: jest.fn() },
  describeModerationError: () => 'moderation failed',
}));
jest.mock('../../../api/profile', () => ({
  PROFILE_PAGE_SIZE: 20,
  profileApi: { getUserProfile: jest.fn() },
}));
jest.mock('../../../api/stories', () => ({
  storiesApi: { deleteStory: jest.fn() },
}));
jest.mock('../../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));

type ElementLike = { type?: unknown; props?: Record<string, unknown> };

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

const PROFILE: ProfileUser = {
  id: 'user-1',
  display_name: 'Nomsa Khan',
  avatar_url: null,
  rooted: null,
  joined_at: '2026-01-01T00:00:00.000Z',
};

const STORY_ACTIVITY: Activity = {
  kind: 'story',
  id: 'story-1',
  created_at: '2026-10-09T12:00:00.000Z',
  payload: {
    title: 'How my grandmother counted the days',
    pillar: 'heritage',
    language: 'zap',
    place: 'Oaxaca',
  },
};

const VERSION_ACTIVITY: Activity = {
  kind: 'version',
  id: 'version-2',
  created_at: '2026-10-10T12:00:00.000Z',
  payload: {
    story_id: 'story-1',
    story_title: 'How my grandmother counted the days',
    language: 'zul',
    version_number: 2,
  },
};

/** Scripts the stubbed `useState` by slot index, keeping initials elsewhere. */
function scriptState(values: Readonly<Record<number, unknown>>): void {
  const last = Math.max(...Object.keys(values).map(Number));
  for (let index = 0; index <= last; index += 1) {
    mockStateValues.push(index in values ? values[index] : mockKeepInitial);
  }
}

/** One render of the wall with the scripted state, exactly as the app builds it. */
function renderScreen(isOwnProfile: boolean, onEditStory: () => void = () => undefined): unknown {
  mockStateSetters.length = 0;

  return UserProfileScreen({
    userId: 'user-1',
    token: 'token',
    isOwnProfile,
    onOpenStory: () => undefined,
    onOpenComment: () => undefined,
    onOpenInquiry: () => undefined,
    onOpenSettings: () => undefined,
    onEditStory,
    onOpenMedia: () => undefined,
    onBack: () => undefined,
    showBackButton: true,
  });
}

/** The card the list builds for one activity. */
function cardFor(tree: unknown, activity: Activity): ElementLike {
  const list = findByType(tree, 'FlatList');
  const renderItem = list?.props?.renderItem as
    ((info: { item: Activity; index: number }) => ElementLike) | undefined;

  return renderItem?.({ item: activity, index: 0 }) as ElementLike;
}

/** The wall in its loaded state with both activity kinds. */
function loaded(extra: Readonly<Record<number, unknown>> = {}): void {
  mockStateValues.length = 0;
  scriptState({
    0: PROFILE,
    1: [STORY_ACTIVITY, VERSION_ACTIVITY],
    2: '',
    3: false,
    ...extra,
  });
}

describe('UserProfileScreen story-card options', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('offers the owner an options control on a story card', () => {
    loaded();
    const card = cardFor(renderScreen(true), STORY_ACTIVITY);

    expect(findByTestID(card, 'card-options-story-1')?.props?.accessibilityLabel).toBe(
      'Options for How my grandmother counted the days',
    );
  });

  it('offers no card options on a version card', () => {
    loaded();
    const card = cardFor(renderScreen(true), VERSION_ACTIVITY);

    expect(findByTestID(card, 'card-options-version-2')).toBeUndefined();
  });

  it('offers no card options to a visitor', () => {
    loaded();
    const card = cardFor(renderScreen(false), STORY_ACTIVITY);

    expect(findByTestID(card, 'card-options-story-1')).toBeUndefined();
  });

  it('opens a menu with Edit and Delete, and hands Edit the story id', () => {
    const onEditStory = jest.fn();
    loaded({ 13: STORY_ACTIVITY });
    const tree = renderScreen(true, onEditStory);

    const edit = findByTestID(tree, 'card-edit');
    expect(edit?.props?.label).toBe('Edit Story');

    (edit?.props?.onPress as () => void)();

    expect(onEditStory).toHaveBeenCalledWith('story-1');
  });

  it('asks before deleting a story from the wall', () => {
    loaded({ 13: STORY_ACTIVITY });
    const tree = renderScreen(true);

    (findByTestID(tree, 'card-delete')?.props?.onPress as () => void)();

    expect(Alert.alert).toHaveBeenCalledTimes(1);
    const [title, message, buttons] = (Alert.alert as jest.Mock).mock.calls[0] as [
      string,
      string,
      { text: string }[],
    ];
    expect(title).toBe('Delete this story?');
    expect(message).toContain('30 days');
    expect(buttons.map((button) => button.text)).toEqual(['Cancel', 'Delete']);
    expect(storiesApi.deleteStory).not.toHaveBeenCalled();
  });

  it('deletes through the server when the question is accepted', async () => {
    (storiesApi.deleteStory as jest.Mock).mockResolvedValue(undefined);
    loaded({ 13: STORY_ACTIVITY });
    const tree = renderScreen(true);

    (findByTestID(tree, 'card-delete')?.props?.onPress as () => void)();
    const buttons = (Alert.alert as jest.Mock).mock.calls[0]?.[2] as
      { text: string; onPress?: () => void }[] | undefined;
    buttons?.find((button) => button.text === 'Delete')?.onPress?.();
    await Promise.resolve();

    expect(storiesApi.deleteStory).toHaveBeenCalledWith('story-1', 'token');
    const updaters = mockStateSetters
      .flatMap((setter) => setter.mock.calls as unknown[][])
      .map(([value]) => value)
      .filter(
        (value): value is (activities: readonly Activity[]) => readonly Activity[] =>
          typeof value === 'function',
      );
    expect(updaters).toHaveLength(1);
    expect(updaters[0]([STORY_ACTIVITY, VERSION_ACTIVITY])).toEqual([VERSION_ACTIVITY]);
  });
});

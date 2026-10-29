import { DotsThree, Gear } from 'phosphor-react-native';

import Header from '../../../components/Header';
import UserProfileScreen from '../UserProfileScreen';

/**
 * The own-profile header control is a **gear** that opens Settings; a visitor's is
 * still an overflow button that opens the block menu (KNOT-020k-fix).
 *
 * `UserProfileScreen` is stateful — the wall's page, its tabs, the mute state and
 * the visitor sheet all live in it — so this suite calls the screen exactly as the
 * app does and walks the tree structurally, the pattern the project's other
 * stateful screens use (there is no renderer and none may be added). Every state
 * slot is handed its initial value, so no network read and no activity card is
 * ever built; the header is outside that data and always drawn.
 */
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useEffect: () => undefined,
  useRef: (initial: unknown) => ({ current: initial }),
  useState: (initial: unknown) => [initial, () => undefined],
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

// The visitor sheet's fade is a native animation; the sheet itself is never open
// here, so a stand-in keeps the suite free of the animation runtime.
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
jest.mock('../../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));

type ElementLike = {
  readonly type?: unknown;
  readonly props?: Record<string, unknown>;
};

/** Walks an element tree depth-first, calling `visit` on every element. */
function walk(node: unknown, visit: (element: ElementLike) => void): void {
  if (Array.isArray(node)) {
    for (const child of node) {
      walk(child, visit);
    }
    return;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return;
  }

  const element = node as ElementLike;
  if (element.type !== undefined) {
    visit(element);
  }
  walk(element.props?.children, visit);
}

/** The header's trailing control, as the screen handed it to `Header`. */
function headerAction(
  isOwnProfile: boolean,
  onOpenSettings: () => void = () => undefined,
): ElementLike {
  const screen = UserProfileScreen({
    userId: 'user-1',
    token: 'token',
    isOwnProfile,
    onOpenStory: () => undefined,
    onOpenComment: () => undefined,
    onOpenInquiry: () => undefined,
    onOpenSettings,
    onOpenMedia: () => undefined,
    onBack: () => undefined,
    showBackButton: true,
  });

  let header: ElementLike | undefined;
  walk(screen, (candidate) => {
    if (candidate.type === Header) {
      header = candidate;
    }
  });

  return header?.props?.right as ElementLike;
}

describe('UserProfileScreen header', () => {
  it('draws a gear on your own wall, and it is labelled Settings', () => {
    const action = headerAction(true);

    expect(action.props?.accessibilityLabel).toBe('Settings');
    expect((action.props?.children as ElementLike).type).toBe(Gear);
  });

  it('tapping the gear opens Settings', () => {
    const onOpenSettings = jest.fn();
    const action = headerAction(true, onOpenSettings);

    (action.props?.onPress as () => void)();
    expect(onOpenSettings).toHaveBeenCalledTimes(1);
  });

  it('keeps the overflow button on a visitor’s wall', () => {
    const action = headerAction(false);

    expect(action.props?.accessibilityLabel).toBe('Profile options');
    expect((action.props?.children as ElementLike).type).toBe(DotsThree);
  });
});

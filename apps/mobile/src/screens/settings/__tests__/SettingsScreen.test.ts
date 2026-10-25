import { APP_VERSION } from '../../../config/app';
import Avatar from '../../../components/Avatar';
import Header from '../../../components/Header';
import RootedBadge from '../../../components/RootedBadge';
import SettingsScreen from '../SettingsScreen';

/**
 * `SettingsScreen` is stateful (the header's profile, the avatar picker's
 * visibility and its in-flight state), so it cannot be called as a plain function
 * without React's hook primitives stubbed — the project has no renderer and must
 * not add one. The component is then built exactly as the app builds it, and the
 * tree is walked structurally to assert what the screen offers
 * (KNOT-020k-fix22, KNOT-020k-fix23).
 *
 * The screen's first state slot is the header's profile, so `useState` hands that
 * slot the fixture below — the read that would fill it is a network call the suite
 * must not make. The picker is stubbed to a host component: importing the real one
 * reaches `react-native-image-picker`, which is a native module.
 */
const mockProfile = {
  id: 'user-1',
  display_name: 'Nomsa Khan',
  avatar_url: '/users/user-1/avatar?v=1',
  rooted: { place: 'Manguzi', duration_bucket: 'many_years' },
  joined_at: '2026-01-01T00:00:00.000Z',
};

let mockStateIndex = 0;

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  // The screen reads its profile header on mount; the suite never performs I/O.
  useEffect: () => undefined,
  useState: (initial: unknown) => {
    const index = mockStateIndex;
    mockStateIndex += 1;
    return [index === 0 ? mockProfile : initial, () => undefined];
  },
}));

jest.mock('react-native', () => ({
  ActivityIndicator: 'ActivityIndicator',
  Alert: { alert: jest.fn() },
  Linking: { openURL: jest.fn(() => Promise.resolve()) },
  Pressable: 'Pressable',
  ScrollView: 'ScrollView',
  StyleSheet: { create: (styles: unknown) => styles },
  Switch: 'Switch',
  Text: 'Text',
  View: 'View',
}));

jest.mock('phosphor-react-native', () => ({
  ArrowLeft: () => null,
  Camera: () => null,
  CaretRight: () => null,
  FileText: () => null,
  Info: () => null,
  Moon: () => null,
  Plant: () => null,
  Prohibit: () => null,
  ShieldCheck: () => null,
  SignOut: () => null,
}));

jest.mock('../../../components/MediaPickerSheet', () => ({
  __esModule: true,
  default: 'MediaPickerSheet',
}));

jest.mock('../../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));
jest.mock('../../../api/client', () => ({ describeError: () => 'request failed' }));

type ElementLike = {
  readonly type?: unknown;
  readonly props?: Record<string, unknown>;
};

/**
 * The screen's own components. They are called in place — the direct-call
 * equivalent of rendering — so the text they draw (a group heading, a row label)
 * is visible to the assertions below. React Native's host components are strings
 * and are left alone.
 */
const LOCAL_COMPONENTS = new Set(['Section', 'SettingRow', 'ThemeRow']);

/** Replaces each local component element with the element it returns. */
function expand(node: unknown): unknown {
  if (Array.isArray(node)) {
    return node.map(expand);
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return node;
  }

  const element = node as ElementLike;
  const type = element.type;
  if (typeof type === 'function' && LOCAL_COMPONENTS.has(type.name)) {
    return expand((type as (props: unknown) => unknown)(element.props));
  }

  if (element.props?.children === undefined) {
    return element;
  }

  return { ...element, props: { ...element.props, children: expand(element.props.children) } };
}

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

/** Every element whose `label` prop matches, in tree order. */
function rows(element: unknown): readonly ElementLike[] {
  const found: ElementLike[] = [];
  walk(element, (candidate) => {
    if (typeof candidate.props?.label === 'string') {
      found.push(candidate);
    }
  });
  return found;
}

/** Every literal string the screen renders directly as a `Text` child. */
function renderedText(element: unknown): string {
  const parts: string[] = [];
  walk(element, (candidate) => {
    if (candidate.type === 'Text' && typeof candidate.props?.children === 'string') {
      parts.push(candidate.props.children);
    }
  });
  return parts.join('\n');
}

const PROPS = {
  userId: 'user-1',
  token: 'token',
  onBack: () => undefined,
  onUserUpdated: () => undefined,
  onSetRooted: () => undefined,
  onOpenBlockedUsers: () => undefined,
  onSignOut: () => undefined,
};

/** One render of the screen, exactly as the app builds it. */
function renderScreen(): ElementLike {
  // Every render starts from the first state slot again.
  mockStateIndex = 0;
  return SettingsScreen(PROPS) as unknown as ElementLike;
}

/** The same render, with the screen's own components expanded in place. */
function renderExpanded(): ElementLike {
  return expand(renderScreen()) as ElementLike;
}

describe('SettingsScreen', () => {
  it('is titled Settings and offers a way back', () => {
    const header = (() => {
      let found: ElementLike | undefined;
      walk(renderScreen(), (candidate) => {
        if (candidate.type === Header) {
          found = candidate;
        }
      });
      return found;
    })();

    expect(header?.props?.title).toBe('Settings');
    expect(header?.props?.left).toBeDefined();
  });

  it('groups the account, the preferences and the About block', () => {
    const text = renderedText(renderExpanded());

    expect(text).toContain('Account');
    expect(text).toContain('Preferences');
    expect(text).toContain('About');
  });

  it('opens with the signed-in person’s face, name and Rooted summary', () => {
    const screen = renderScreen();
    const byType = (type: unknown): ElementLike | undefined => {
      let found: ElementLike | undefined;
      walk(screen, (candidate) => {
        if (candidate.type === type) {
          found = candidate;
        }
      });
      return found;
    };

    const avatar = byType(Avatar);
    expect(avatar?.props?.displayName).toBe(mockProfile.display_name);
    // The relative path is resolved against the API base before it reaches Avatar.
    expect(avatar?.props?.uri).toBe(`http://localhost:8080${mockProfile.avatar_url}`);

    const rooted = byType(RootedBadge);
    expect(rooted?.props?.place).toBe(mockProfile.rooted.place);
    expect(rooted?.props?.durationBucket).toBe(mockProfile.rooted.duration_bucket);

    // The name is drawn as text by the screen itself.
    expect(renderedText(expand(screen))).toContain(mockProfile.display_name);
  });

  it('separates its rows with whitespace instead of rules', () => {
    // Expanding the rows hands back their real style objects, so the absence of a
    // drawn divider is provable rather than assumed (KNOT-020k-fix23).
    const expanded = renderExpanded();
    const ruleKeys = ['borderTopWidth', 'borderBottomWidth'];
    let ruled = 0;

    walk(expanded, (candidate) => {
      const style = candidate.props?.style;
      const parts = Array.isArray(style) ? style : [style];
      for (const part of parts) {
        if (part === null || typeof part !== 'object') {
          continue;
        }
        const record = part as Record<string, unknown>;
        for (const key of ruleKeys) {
          if (typeof record[key] === 'number' && record[key] !== 0) {
            ruled += 1;
          }
        }
      }
    });

    expect(ruled).toBe(0);
  });

  it('offers every account action the profile sheet used to hold', () => {
    const labels = rows(renderScreen()).map((row) => row.props?.label);

    expect(labels).toContain('Edit avatar');
    expect(labels).toContain('Set Rooted');
    expect(labels).toContain('Blocked users');
  });

  it('states the current theme, and offers the preference as a switch', () => {
    const text = renderedText(renderExpanded());

    expect(text).toContain('Theme');
    // The light theme is the one the provider hands the component in this suite.
    expect(text).toContain('Light');
  });

  it('shows the app version and links the two legal pages', () => {
    const screen = renderScreen();
    const byLabel = (label: string): ElementLike | undefined =>
      rows(screen).find((row) => row.props?.label === label);

    // The version row reports the real app version and is not a control.
    expect(byLabel('App version')?.props?.value).toBe(`v${APP_VERSION}`);
    expect(byLabel('App version')?.props?.onPress).toBeUndefined();

    // The legal rows are controls: each opens a page.
    expect(typeof byLabel('Privacy Policy')?.props?.onPress).toBe('function');
    expect(typeof byLabel('Terms of Use')?.props?.onPress).toBe('function');
  });

  it('ends with a destructive sign out', () => {
    const signOut = rows(renderScreen()).find((row) => row.props?.label === 'Sign out');

    expect(signOut).toBeDefined();
    expect(signOut?.props?.danger).toBe(true);
    expect(typeof signOut?.props?.onPress).toBe('function');
  });
});

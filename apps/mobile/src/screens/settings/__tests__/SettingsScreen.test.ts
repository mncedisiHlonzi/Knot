import { APP_VERSION } from '../../../config/app';
import Avatar from '../../../components/Avatar';
import Header from '../../../components/Header';
import SettingsScreen from '../SettingsScreen';
import { colors, radius } from '../../../theme';

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

/** The profile the header reads in a given test; a test may swap out the Rooted
 * summary to exercise the card's empty state (KNOT-020k-fix). */
type MockProfile = typeof mockProfile;
let mockProfileValue: MockProfile | (Omit<MockProfile, 'rooted'> & { rooted: null }) = mockProfile;

let mockStateIndex = 0;
const mockSetMode = jest.fn();
const mockMode = { value: 'system' as 'system' | 'light' | 'dark' };

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => ({
    ...jest.requireActual('../../../theme/ThemeContext').lightTheme,
    mode: mockMode.value,
    setMode: mockSetMode,
  }),
  useCallback: (callback: unknown) => callback,
  // The screen reads its profile header on mount; the suite never performs I/O.
  useEffect: () => undefined,
  useState: (initial: unknown) => {
    const index = mockStateIndex;
    mockStateIndex += 1;
    return [index === 0 ? mockProfileValue : initial, () => undefined];
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
  DeviceMobile: () => null,
  Devices: () => null,
  FileText: () => null,
  Info: () => null,
  Moon: () => null,
  Plant: () => null,
  Prohibit: () => null,
  ShieldCheck: () => null,
  Star: () => null,
  Sun: () => null,
  Trash: () => null,
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
const LOCAL_COMPONENTS = new Set(['SectionHeading', 'SettingRow']);

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
  email: 'nomsa@example.com',
  token: 'token',
  onBack: () => undefined,
  onUserUpdated: () => undefined,
  onSetRooted: () => undefined,
  onOpenBlockedUsers: () => undefined,
  onOpenSessions: () => undefined,
  onOpenDeleteAccount: () => undefined,
  onOpenLegal: jest.fn(),
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
  beforeEach(() => {
    mockProfileValue = mockProfile;
    mockMode.value = 'system';
  });

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

  it('groups the account, the preferences, privacy and the About block', () => {
    const text = renderedText(renderExpanded());

    expect(text).toContain('Account');
    expect(text).toContain('Appearance');
    expect(text).toContain('Privacy');
    expect(text).toContain('About');
  });

  it('opens with the signed-in person’s face, name and address', () => {
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
    // 48 points (the `lg` size): the block is a header, not a profile wall.
    expect(avatar?.props?.size).toBe('lg');

    // The name and the address are drawn as text by the screen itself.
    const text = renderedText(expand(screen));
    expect(text).toContain(mockProfile.display_name);
    expect(text).toContain('nomsa@example.com');
  });

  it('draws the Rooted status card with the place and a View pill', () => {
    const text = renderedText(renderExpanded());

    expect(text).toContain('Rooted Status');
    expect(text).toContain('Rooted in Manguzi');
    expect(text).toContain('View');
  });

  it('offers to set the Rooted place when the person has none', () => {
    mockProfileValue = { ...mockProfile, rooted: null };
    const text = renderedText(renderExpanded());

    expect(text).toContain('Become Rooted');
    expect(text).toContain('Unlock advanced features');
    expect(text).toContain('Set up');
  });

  // The header is a centred column: the name and the Rooted summary sit under the
  // face rather than beside it (KNOT-020k-fix25).
  it('centres the name under the avatar', () => {
    let nameStyle: unknown;

    walk(renderScreen(), (candidate) => {
      if (candidate.type === 'Text' && candidate.props?.children === mockProfile.display_name) {
        nameStyle = candidate.props.style;
      }
    });

    expect(nameStyle).toEqual(
      expect.arrayContaining([expect.objectContaining({ textAlign: 'center' })]),
    );
  });

  // Every part of the block sits on one centre line: the header is a centred
  // column, and the avatar — which pins itself left in its own style — is given a
  // wrapper that centres it (KNOT-020k-fix26).
  it('centres the whole header block, avatar included', () => {
    let headerStyle: unknown;
    let avatarWrapperStyle: unknown;

    walk(renderScreen(), (candidate) => {
      const raw = candidate.props?.children;
      const children = (Array.isArray(raw) ? raw : [raw]) as readonly (
        ElementLike | null | undefined
      )[];

      if (children.some((child) => child?.props?.children === mockProfile.display_name)) {
        headerStyle = candidate.props?.style;
      }
      if (children.some((child) => child?.type === Avatar)) {
        avatarWrapperStyle = candidate.props?.style;
      }
    });

    expect(headerStyle).toMatchObject({ alignItems: 'center' });
    // A column, never a row: a row would put the text beside the face again.
    expect(headerStyle).not.toMatchObject({ flexDirection: 'row' });

    expect(avatarWrapperStyle).toMatchObject({ alignSelf: 'center' });
  });

  // The groups are bento containers: a raised surface with a hairline edge and a
  // large corner, parted inside by hairlines rather than by whitespace alone
  // (KNOT-020k-fix).
  it('draws each group as a rounded card with hairlines between its rows', () => {
    const expanded = renderExpanded();
    const parts: Record<string, unknown>[] = [];

    walk(expanded, (candidate) => {
      const style = candidate.props?.style;
      for (const part of Array.isArray(style) ? style : [style]) {
        if (part !== null && typeof part === 'object') {
          parts.push(part as Record<string, unknown>);
        }
      }
    });

    // The status card and the four groups share one container shape.
    const cards = parts.filter(
      (part) =>
        part.borderRadius === radius.xxl &&
        part.borderWidth === 1 &&
        part.backgroundColor === colors.bg.raised,
    );
    expect(cards).toHaveLength(5);

    // Five rows are parted from the row above them: Set Rooted, Sessions and
    // Delete account in Account, and the two legal rows in About.
    const dividers = parts.filter(
      (part) => part.borderTopWidth === 1 && part.borderTopColor === colors.border.hairline,
    );
    expect(dividers).toHaveLength(5);
  });

  it('offers every account action the profile sheet used to hold', () => {
    const labels = rows(renderScreen()).map((row) => row.props?.label);

    expect(labels).toContain('Edit avatar');
    expect(labels).toContain('Set Rooted');
    expect(labels).toContain('Blocked users');
  });

  it('offers System, Light and Dark, and applies a choice on the tap', () => {
    // The Appearance rows are radios, not a switch: three states cannot live on a
    // two-state control (KNOT-020k).
    const radios: ElementLike[] = [];
    walk(renderExpanded(), (candidate) => {
      if (candidate.props?.accessibilityRole === 'radio') {
        radios.push(candidate);
      }
    });

    expect(radios.map((radio) => radio.props?.accessibilityLabel)).toEqual([
      'System',
      'Light',
      'Dark',
    ]);

    jest.clearAllMocks();
    (radios[2]?.props?.onPress as () => void)();
    expect(mockSetMode).toHaveBeenCalledWith('dark');

    (radios[1]?.props?.onPress as () => void)();
    expect(mockSetMode).toHaveBeenCalledWith('light');

    (radios[0]?.props?.onPress as () => void)();
    expect(mockSetMode).toHaveBeenCalledWith('system');
  });

  // The active pill is the primary fill with inverse ink; the other two are
  // sunken with primary ink, so the choice reads in both modes (KNOT-020k-fix).
  it('fills the active appearance pill and sinks the others', () => {
    mockMode.value = 'dark';

    const radios: ElementLike[] = [];
    walk(renderExpanded(), (candidate) => {
      if (candidate.props?.accessibilityRole === 'radio') {
        radios.push(candidate);
      }
    });

    const styleOf = (index: number): Record<string, unknown>[] => {
      const style = radios[index]?.props?.style;
      return (Array.isArray(style) ? style : [style]).filter(
        (part): part is Record<string, unknown> => part !== null && typeof part === 'object',
      );
    };

    expect(radios[2]?.props?.accessibilityState).toEqual({ selected: true });
    expect(styleOf(2)).toEqual(
      expect.arrayContaining([expect.objectContaining({ backgroundColor: colors.brand.primary })]),
    );
    expect(styleOf(0)).toEqual(
      expect.arrayContaining([expect.objectContaining({ backgroundColor: colors.bg.sunken })]),
    );
  });

  it('shows the app version and links the two legal pages', () => {
    const screen = renderScreen();
    const byLabel = (label: string): ElementLike | undefined =>
      rows(screen).find((row) => row.props?.label === label);

    // The version row reports the real app version and is not a control.
    expect(byLabel('App version')?.props?.value).toBe(`v${APP_VERSION}`);
    expect(byLabel('App version')?.props?.onPress).toBeUndefined();

    // The legal rows are controls, and each opens the document in the app rather
    // than handing a URL to the browser (KNOT-035).
    jest.clearAllMocks();
    expect(typeof byLabel('Privacy Policy')?.props?.onPress).toBe('function');
    expect(typeof byLabel('Terms of Service')?.props?.onPress).toBe('function');

    (byLabel('Privacy Policy')?.props?.onPress as () => void)();
    expect(PROPS.onOpenLegal).toHaveBeenCalledWith('privacy');

    (byLabel('Terms of Service')?.props?.onPress as () => void)();
    expect(PROPS.onOpenLegal).toHaveBeenCalledWith('terms');
  });

  it('ends with a red text sign out, outside the containers', () => {
    const screen = renderScreen();

    // It is no longer an icon row: the danger ink carries it on its own.
    expect(rows(screen).find((row) => row.props?.label === 'Sign out')).toBeUndefined();

    let signOutLabelStyle: unknown;
    let signOutControl: ElementLike | undefined;
    walk(screen, (candidate) => {
      if (candidate.props?.accessibilityLabel === 'Sign out') {
        signOutControl = candidate;
      }
      if (candidate.type === 'Text' && candidate.props?.children === 'Sign out') {
        signOutLabelStyle = candidate.props?.style;
      }
    });

    expect(typeof signOutControl?.props?.onPress).toBe('function');
    expect(signOutLabelStyle).toEqual(
      expect.arrayContaining([expect.objectContaining({ color: colors.state.danger })]),
    );
  });
});

import { APP_VERSION } from '../../../config/app';
import Header from '../../../components/Header';
import SettingsScreen from '../SettingsScreen';

/**
 * `SettingsScreen` is stateful (the avatar picker's visibility and its in-flight
 * state), so it cannot be called as a plain function without React's hook
 * primitives stubbed — the project has no renderer and must not add one. The
 * component is then built exactly as the app builds it, and the tree is walked
 * structurally to assert what the screen offers (KNOT-020k-fix22).
 *
 * The picker is stubbed to a host component: importing the real one reaches
 * `react-native-image-picker`, which is a native module.
 */
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useState: (initial: unknown) => [initial, () => undefined],
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
  token: 'token',
  onBack: () => undefined,
  onUserUpdated: () => undefined,
  onSetRooted: () => undefined,
  onOpenBlockedUsers: () => undefined,
  onSignOut: () => undefined,
};

/** One render of the screen, exactly as the app builds it. */
function renderScreen(): ElementLike {
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

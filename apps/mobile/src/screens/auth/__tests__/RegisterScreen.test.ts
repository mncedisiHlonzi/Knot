import Button from '../../../components/Button';
import RegisterScreen from '../RegisterScreen';

/**
 * `RegisterScreen` is stateful, so it cannot be called as a plain function without
 * React's hook primitives stubbed — the project has no renderer and must not add
 * one. The consent box is the tenth state slot (it was appended on purpose, so the
 * earlier slots did not move), and this suite scripts exactly that slot to render
 * the screen with the box ticked or unticked (KNOT-035).
 */
const mockConsentStateIndex = 9;

let mockStateIndex = 0;
let mockConsented = false;

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../../theme/ThemeContext').lightTheme,
  useState: (initial: unknown) => {
    const index = mockStateIndex;
    mockStateIndex += 1;
    return [index === mockConsentStateIndex ? mockConsented : initial, () => undefined];
  },
}));

jest.mock('react-native', () => ({
  Pressable: 'Pressable',
  ScrollView: 'ScrollView',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

jest.mock('phosphor-react-native', () => ({
  CheckSquare: 'CheckSquare',
  Square: 'Square',
}));

jest.mock('../../../components/LanguagePicker', () => ({
  __esModule: true,
  default: 'LanguagePicker',
}));
jest.mock('../../../components/Logomark', () => ({ __esModule: true, default: 'Logomark' }));
jest.mock('../../../api/auth', () => ({ register: jest.fn() }));
jest.mock('../../../api/client', () => ({ describeError: () => 'request failed' }));

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

/** Every element a walk finds. */
function elements(tree: unknown): readonly ElementLike[] {
  const found: ElementLike[] = [];
  walk(tree, (element) => found.push(element));
  return found;
}

/** One render of the screen, with consent either given or not. */
function renderScreen(
  consented: boolean,
  onOpenLegal: (document: string) => void = () => undefined,
): ElementLike {
  mockStateIndex = 0;
  mockConsented = consented;

  return RegisterScreen({
    onSwitchToLogin: () => undefined,
    onOpenVerifyEmail: () => undefined,
    onOpenLegal,
  }) as unknown as ElementLike;
}

/** The submit action, found by its label. */
function createAccountButton(tree: ElementLike): ElementLike | undefined {
  return elements(tree).find(
    (element) => element.type === Button && element.props?.label === 'Create account',
  );
}

/** The consent checkbox control. */
function consentBox(tree: ElementLike): ElementLike | undefined {
  return elements(tree).find((element) => element.props?.testID === 'consent-checkbox');
}

describe('RegisterScreen consent', () => {
  it('keeps the Create account button disabled until the box is ticked', () => {
    expect(createAccountButton(renderScreen(false))?.props?.disabled).toBe(true);
    expect(createAccountButton(renderScreen(true))?.props?.disabled).toBe(false);
  });

  it('starts with the box unticked', () => {
    const box = consentBox(renderScreen(false));

    expect(box?.props?.accessibilityRole).toBe('checkbox');
    expect(box?.props?.accessibilityState).toEqual({ checked: false });

    // The empty square is the unticked control.
    const icons = elements(renderScreen(false)).filter(
      (element) => element.type === 'Square' || element.type === 'CheckSquare',
    );
    expect(icons.map((icon) => icon.type)).toEqual(['Square']);
  });

  it('draws a ticked box once consent is given', () => {
    expect(consentBox(renderScreen(true))?.props?.accessibilityState).toEqual({ checked: true });

    const icons = elements(renderScreen(true)).filter(
      (element) => element.type === 'Square' || element.type === 'CheckSquare',
    );
    expect(icons.map((icon) => icon.type)).toEqual(['CheckSquare']);
  });

  it('names both documents and opens each one from the sentence', () => {
    const opened: string[] = [];
    const tree = renderScreen(false, (document: string) => opened.push(document));

    const link = (label: string): ElementLike | undefined =>
      elements(tree).find(
        (element) => element.type === 'Text' && element.props?.children === label,
      );

    expect(link('Terms of Service')?.props?.accessibilityRole).toBe('link');
    expect(link('Privacy Policy')?.props?.accessibilityRole).toBe('link');

    (link('Terms of Service')?.props?.onPress as (() => void) | undefined)?.();
    (link('Privacy Policy')?.props?.onPress as (() => void) | undefined)?.();

    expect(opened).toEqual(['terms', 'privacy']);
  });

  it('gives the checkbox an accessible name that states the agreement', () => {
    expect(consentBox(renderScreen(false))?.props?.accessibilityLabel).toBe(
      'I agree to the Terms of Service and Privacy Policy',
    );
  });
});

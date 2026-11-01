import Button from '../../../components/Button';
import Header from '../../../components/Header';
import LanguagePicker from '../../../components/LanguagePicker';
import LocationPicker from '../../../components/LocationPicker';
import { colors, typeScale } from '../../../theme';
import AskInquiryScreen from '../AskInquiryScreen';

/**
 * The Ask form is text-first (KNOT-020k-fix14): a bar with close and Ask, two
 * metadata chips that open the existing pickers, and two borderless fields. This
 * project has no renderer and must not add one, so the screen is called as a plain
 * function with React's hook primitives stubbed: `useState` returns the initial
 * value unless a test scripts that slot with `scriptState`, and nothing is
 * fetched. The pickers and the API are stubbed, so the element tree holds their
 * elements without invoking them.
 */
const mockKeepInitial = Symbol('keep initial value');
const mockStateValues: unknown[] = [];
const mockStateSetters: jest.Mock[] = [];

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../../theme/ThemeContext').lightTheme,
  useState: (initial: unknown) => {
    const setter = jest.fn();
    mockStateSetters.push(setter);
    const scripted = mockStateValues.length > 0 ? mockStateValues.shift() : mockKeepInitial;
    return [scripted === mockKeepInitial ? initial : scripted, setter];
  },
}));

jest.mock('react-native', () => ({
  // Chip builds its animated pressable at module scope.
  Animated: {
    Value: class {
      setValue(): void {}
      interpolate(): number {
        return 0;
      }
    },
    View: 'Animated.View',
    createAnimatedComponent: <T>(component: T): T => component,
    spring: () => ({ start: () => undefined, stop: () => undefined }),
    timing: () => ({ start: () => undefined, stop: () => undefined }),
  },
  Easing: { bezier: () => (progress: number) => progress },
  KeyboardAvoidingView: 'KeyboardAvoidingView',
  Platform: { OS: 'android', select: (options: { android?: unknown }) => options.android },
  Pressable: 'Pressable',
  ScrollView: 'ScrollView',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  TextInput: 'TextInput',
  View: 'View',
}));

jest.mock('phosphor-react-native', () => ({
  Globe: () => null,
  ImageSquare: () => null,
  MapPin: () => null,
  X: () => null,
}));

jest.mock('../../../components/LanguagePicker', () => ({
  __esModule: true,
  default: 'LanguagePicker',
}));
jest.mock('../../../components/LocationPicker', () => ({
  __esModule: true,
  default: 'LocationPicker',
}));

jest.mock('../../../api/client', () => ({ describeError: () => 'request failed' }));
jest.mock('../../../api/inquiries', () => ({
  inquiriesApi: { createInquiry: jest.fn() },
}));

type ElementLike = { type?: unknown; props?: Record<string, unknown> };

/** Every element of the given type under `node`, in tree order. */
function elementsByType(node: unknown, type: unknown): ElementLike[] {
  if (Array.isArray(node)) {
    return node.flatMap((child) => elementsByType(child, type));
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return [];
  }

  const element = node as ElementLike;
  const here = element.type === type ? [element] : [];

  return [...here, ...elementsByType(element.props?.children, type)];
}

/** The first element of the given type under `node`. */
function findElementByType(node: unknown, type: unknown): ElementLike | undefined {
  return elementsByType(node, type)[0];
}

/** The first element whose props satisfy the predicate, children only. */
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

/** The first element carrying a `testID`. */
function findByTestID(node: unknown, testID: string): ElementLike | undefined {
  return findElement(node, (props) => props.testID === testID);
}

/** The first element whose `children` is exactly this string. */
function findText(node: unknown, text: string): ElementLike | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findText(child, text);
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
  if (element.props?.children === text) {
    return element;
  }

  return findText(element.props?.children, text);
}

/**
 * Scripts the stubbed `useState` by slot index, in hook order: title 0, body 1,
 * language 2, locationText 3, selectedPlace 4, **openPicker 5**, submitting 6,
 * error 7.
 */
function scriptState(values: Readonly<Record<number, unknown>>): void {
  const last = Math.max(...Object.keys(values).map((key) => Number(key)));
  for (let index = 0; index <= last; index += 1) {
    mockStateValues.push(index in values ? values[index] : mockKeepInitial);
  }
}

/** Every value any `useState` setter received, in call order. */
function stateWrites(): unknown[] {
  return mockStateSetters.flatMap((setter) =>
    setter.mock.calls.map((call) => (call as unknown[])[0]),
  );
}

const PROPS = {
  token: 'token',
  defaultLanguage: 'zul',
  onAsked: () => undefined,
  onCancel: () => undefined,
};

beforeEach(() => {
  mockStateValues.length = 0;
  mockStateSetters.length = 0;
});

describe('AskInquiryScreen bar', () => {
  it('carries only a close control and the Ask action', () => {
    const header = findElementByType(AskInquiryScreen(PROPS), Header);
    const close = findElement(header?.props?.left, (props) => props.accessibilityLabel === 'Close');
    const ask = findElementByType(header?.props?.right, Button);

    // The screen's own title is gone: the question being written is the title
    // (KNOT-020k-fix14).
    expect(header?.props?.title).toBeUndefined();
    expect(close).toBeDefined();
    expect(ask?.props?.label).toBe('Ask');
    expect(ask?.props?.variant).toBe('primary');
  });

  it('closes the composer from the bar', () => {
    const onCancel = jest.fn();
    const header = findElementByType(AskInquiryScreen({ ...PROPS, onCancel }), Header);
    const close = findElement(header?.props?.left, (props) => props.accessibilityLabel === 'Close');

    (close?.props?.onPress as () => void)();

    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});

describe('AskInquiryScreen metadata', () => {
  it('offers the language and the place as outlined chips with glyphs', () => {
    const tree = AskInquiryScreen(PROPS);
    const language = findByTestID(tree, 'ask-language-chip');
    const place = findByTestID(tree, 'ask-place-chip');

    expect(language?.props?.label).toBe('Zulu');
    expect(language?.props?.variant).toBe('outline');
    expect(language?.props?.icon).toBeDefined();

    // No place yet, so the chip names the field rather than a value.
    expect(place?.props?.label).toBe('Place');
    expect(place?.props?.variant).toBe('outline');
    expect(place?.props?.icon).toBeDefined();
  });

  it('opens the picker a chip names, and only that one', () => {
    scriptState({ 5: 'place' });
    const tree = AskInquiryScreen(PROPS);

    expect(findElementByType(tree, LocationPicker)).toBeDefined();
    expect(findElementByType(tree, LanguagePicker)).toBeUndefined();
  });

  it('records the picker a tapped chip asks for', () => {
    const tree = AskInquiryScreen(PROPS);
    const place = findByTestID(tree, 'ask-place-chip');

    (place?.props?.onPress as () => void)();

    expect(stateWrites()).toContain('place');
  });
});

describe('AskInquiryScreen fields', () => {
  it('draws the question borderlessly at the headline step', () => {
    const title = findByTestID(AskInquiryScreen(PROPS), 'ask-title');
    const style = title?.props?.style as readonly unknown[];
    const overrides = style?.[1] as Record<string, unknown> | undefined;

    expect(title?.props?.placeholder).toBe(
      'Why do the older women wear red ribbons in their braids?',
    );
    expect(style?.[0]).toBe(typeScale.h1);
    // Borderless: no side, no bottom rule — the placeholder IS the label.
    expect(overrides).not.toHaveProperty('borderWidth');
    expect(overrides).not.toHaveProperty('borderBottomWidth');
  });

  it('draws the detail borderlessly at the reading step', () => {
    const body = findByTestID(AskInquiryScreen(PROPS), 'ask-body');
    const style = body?.props?.style as readonly unknown[];

    expect(body?.props?.placeholder).toBe('At the festival almost every woman...');
    expect(body?.props?.multiline).toBe(true);
    expect(style?.[0]).toBe(typeScale.reading);
    expect(style?.[1]).not.toHaveProperty('borderWidth');
  });

  it('reports a missing question instead of sending it', () => {
    const header = findElementByType(AskInquiryScreen(PROPS), Header);
    const ask = findElementByType(header?.props?.right, Button);

    (ask?.props?.onPress as () => void)();

    expect(stateWrites()).toContain('A question title is required.');
  });
});

describe('AskInquiryScreen footer', () => {
  it('names the people a place routes the question to', () => {
    scriptState({ 3: 'Manguzi' });
    const footer = findByTestID(AskInquiryScreen(PROPS), 'ask-footer');
    const note = findText(footer, 'People rooted in Manguzi will be notified.');

    expect(note).toBeDefined();
    expect((note?.props?.style as readonly unknown[])[1]).toMatchObject({
      color: colors.text.tertiary,
    });
  });

  it('says plainly that a place-less question notifies nobody', () => {
    const footer = findByTestID(AskInquiryScreen(PROPS), 'ask-footer');
    const note = findElement(
      footer,
      (props) =>
        typeof props.children === 'string' && props.children.includes('nobody is notified'),
    );

    expect(note).toBeDefined();
  });
});

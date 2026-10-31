import Button from '../../../components/Button';
import Chip from '../../../components/Chip';
import Input from '../../../components/Input';
import { typeScale } from '../../../theme';
import InquiryListScreen from '../InquiryListScreen';

/**
 * `InquiryListScreen` owns the Inquiries hero — the invitation to ask, the place
 * field with its Ask action, and the starter chips — plus the list under it. This
 * project has no renderer and must not add one, so the screen is called as a plain
 * function with React's hook primitives stubbed: `useCallback` returns its
 * callback unchanged, `useEffect` never runs (so nothing is fetched) and `useState`
 * returns the initial value. Nothing below invokes the components the hero holds;
 * their props are read from the element tree (KNOT-020k-fix9).
 */
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useEffect: () => undefined,
  useRef: (value: unknown) => ({ current: value }),
  useState: (initial: unknown) => [initial, jest.fn()],
}));

jest.mock('react-native', () => ({
  ActivityIndicator: 'ActivityIndicator',
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
  FlatList: 'FlatList',
  Pressable: 'Pressable',
  RefreshControl: 'RefreshControl',
  ScrollView: 'ScrollView',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

jest.mock('phosphor-react-native', () => ({
  MapPin: () => null,
  X: () => null,
}));

jest.mock('../../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));
jest.mock('../../../api/client', () => ({ describeError: () => 'request failed' }));
jest.mock('../../../api/inquiries', () => ({
  INQUIRY_PAGE_SIZE: 20,
  inquiriesApi: { listInquiries: jest.fn() },
}));

type ElementLike = {
  type?: unknown;
  props?: Record<string, unknown>;
};

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

/** The first element carrying a `children` string equal to `text`. */
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

const PROPS = {
  token: 'token',
  onOpenInquiry: () => undefined,
  onAsk: () => undefined,
  onOpenUserProfile: () => undefined,
};

/** The hero, as the list's header element (the element is not invoked). */
function heroFor(tree: unknown): unknown {
  const list = findElementByType(tree, 'FlatList');
  return list?.props?.ListHeaderComponent;
}

describe('InquiryListScreen hero', () => {
  it('opens with the question, not with the screen title', () => {
    const hero = heroFor(InquiryListScreen(PROPS));
    const headline = findText(hero, 'What are you curious about?');
    const subtitle = findText(
      hero,
      'Ask about a place, a tradition or a word. People rooted there will answer.',
    );

    expect(headline).toBeDefined();
    // The largest type step, in the primary ink, is what makes it the hero.
    expect((headline?.props?.style as readonly unknown[])[0]).toBe(typeScale.h1);
    expect(subtitle).toBeDefined();
    expect((subtitle?.props?.style as readonly unknown[])[0]).toBe(typeScale.body);
  });

  it('puts the place field and its Ask action on one row', () => {
    const onAsk = jest.fn();
    const hero = heroFor(InquiryListScreen({ ...PROPS, onAsk }));
    const field = findElementByType(hero, Input);
    const ask = elementsByType(hero, Button);

    expect(field).toBeDefined();
    expect(field?.props?.placeholder).toBe('Which place?');
    // The pin is what makes an underline field read as a place rather than a search.
    expect(field?.props?.leadingIcon).toBeDefined();

    expect(ask).toHaveLength(1);
    expect(ask[0]?.props?.label).toBe('Ask');
    expect(ask[0]?.props?.variant).toBe('primary');

    // An empty field still asks — the place is optional, the question is not.
    (ask[0]?.props?.onPress as () => void)();
    expect(onAsk).toHaveBeenCalledWith(undefined);
  });

  it('hands the starters to the composer as opening words', () => {
    const onAsk = jest.fn();
    const hero = heroFor(InquiryListScreen({ ...PROPS, onAsk }));
    const chips = elementsByType(hero, Chip);
    const labels = chips.map((chip) => chip.props?.label);

    expect(labels).toEqual(['Why do people…', 'What is the name of…', 'Who taught you…']);

    (chips[0]?.props?.onPress as () => void)();
    expect(onAsk).toHaveBeenCalledWith({ title: 'Why do people…' });
  });

  it('offers the place filter only once there is something to narrow by', () => {
    // With nothing typed the hero is the headline, the field and the starters: the
    // chips are the starters alone.
    const hero = heroFor(InquiryListScreen(PROPS));

    expect(elementsByType(hero, Chip)).toHaveLength(3);
  });

  it('keeps the questions below the hero', () => {
    const tree = InquiryListScreen(PROPS);
    const list = findElementByType(tree, 'FlatList');
    const renderItem = list?.props?.renderItem as (info: {
      item: { id: string; title: string; body: string; answer_count: number };
    }) => ElementLike;
    const row = renderItem({
      item: {
        id: 'inquiry-1',
        title: 'Why do the older women wear red ribbons?',
        body: 'At the festival almost every woman over sixty had one.',
        answer_count: 6,
      },
    });

    expect(findText(row, 'Why do the older women wear red ribbons?')).toBeDefined();
    expect(findText(row, '6 answers') ?? findText(row, '6 answers · eng')).toBeDefined();
  });
});

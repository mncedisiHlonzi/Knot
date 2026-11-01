import { MagnifyingGlass, MapPin } from 'phosphor-react-native';

import type { Inquiry } from '../../../api/inquiries';
import Avatar from '../../../components/Avatar';
import Button from '../../../components/Button';
import Chip from '../../../components/Chip';
import EmptyState from '../../../components/EmptyState';
import Header from '../../../components/Header';
import { colors, radius, typeScale } from '../../../theme';
import InquiryListScreen from '../InquiryListScreen';

/**
 * The value the stubbed `useState` returns for a slot a test does not script, so a
 * hook that runs more often than the queue is long keeps its initial value.
 */
const mockKeepInitial = Symbol('keep initial value');

/** The `useState` returns a test scripts, consumed in hook order. */
const mockStateValues: unknown[] = [];

/** Every setter the stubbed `useState` handed out, in hook order. */
const mockStateSetters: jest.Mock[] = [];

/**
 * `InquiryListScreen` owns the Inquiries hero — the invitation to ask, the place
 * pill with its nested Ask action, the starter chips, and the inline search the bar
 * reveals — plus the list under it. This project has no renderer and must not add
 * one, so the screen is called as a plain function with React's hook primitives
 * stubbed: `useCallback` returns its callback unchanged, `useEffect` never runs (so
 * nothing is fetched) and `useState` returns the initial value unless a test has
 * scripted that slot with `scriptState`. Nothing below invokes the components the
 * hero holds; their props are read from the element tree (KNOT-020k-fix9,
 * KNOT-020k-fix11).
 */
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useEffect: () => undefined,
  useRef: (value: unknown) => ({ current: value }),
  useState: (initial: unknown) => {
    const setter = jest.fn();
    mockStateSetters.push(setter);
    const scripted = mockStateValues.length > 0 ? mockStateValues.shift() : mockKeepInitial;
    return [scripted === mockKeepInitial ? initial : scripted, setter];
  },
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
  // Avatar draws its image-or-initials frame with both natives.
  Image: 'Image',
  Pressable: 'Pressable',
  RefreshControl: 'RefreshControl',
  ScrollView: 'ScrollView',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  // The shared underline `Input` renders one; the hero no longer does
  // (KNOT-020k-fix13).
  TextInput: 'TextInput',
  View: 'View',
}));

jest.mock('phosphor-react-native', () => ({
  MagnifyingGlass: () => null,
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
  viewer: { display_name: 'Nomsa Khan', avatar_url: '' },
  onOpenInquiry: () => undefined,
  onAsk: () => undefined,
  onOpenUserProfile: () => undefined,
  onOpenProfile: () => undefined,
};

/**
 * Scripts the stubbed `useState` by slot index, in hook order.
 *
 * The queue is consumed from the front, so an index a test does not name keeps its
 * own initial value. The screen's order is: inquiries 0, nextCursor 1, loading 2,
 * refreshing 3, loadingMore 4, error 5, **searchOpen 6**, **query 7**
 * (KNOT-020k-fix11, KNOT-020k-fix13).
 */
function scriptState(values: Readonly<Record<number, unknown>>): void {
  const last = Math.max(...Object.keys(values).map((key) => Number(key)));
  for (let index = 0; index <= last; index += 1) {
    mockStateValues.push(index in values ? values[index] : mockKeepInitial);
  }
}

/** An inquiry as the list receives it. */
const inquiryFixture = (id: string, title: string, body: string, place: string): Inquiry => ({
  id,
  author_id: `author-${id}`,
  title,
  body,
  language: 'eng',
  place,
  place_country: null,
  latitude: null,
  longitude: null,
  answer_count: 1,
  created_at: '2026-10-09T12:00:00.000Z',
  updated_at: '2026-10-09T12:00:00.000Z',
});

beforeEach(() => {
  mockStateValues.length = 0;
  mockStateSetters.length = 0;
});

/** Finds the first element whose props satisfy the predicate, children only. */
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

/** The first element carrying a `testID` equal to the given one. */
function findByTestID(node: unknown, testID: string): ElementLike | undefined {
  return findElement(node, (props) => props.testID === testID);
}

/** The hero, as the list's header element (the element is not invoked). */
function heroFor(tree: unknown): unknown {
  const list = findElementByType(tree, 'FlatList');
  return list?.props?.ListHeaderComponent;
}

describe('InquiryListScreen header', () => {
  it('reads on the leading edge, with search and the avatar on the trailing one', () => {
    const header = findElementByType(InquiryListScreen(PROPS), Header);
    const right = header?.props?.right;
    const search = findElement(right, (props) => props.accessibilityLabel === 'Search questions');

    // The reference draws a page title, not a centred label (KNOT-020k-fix11).
    expect(header?.props?.title).toBe('Inquiries');
    expect(header?.props?.titleAlign).toBe('left');

    expect(search).toBeDefined();
    expect(findElementByType(right, Avatar)).toBeDefined();
    // Asking is the hero's job: the bar keeps no action of its own beside the pair
    // (KNOT-020k-fix10).
    expect(findText(right, 'Ask a question')).toBeUndefined();
  });

  it('draws the search glyph in the primary ink at the bold weight', () => {
    const header = findElementByType(InquiryListScreen(PROPS), Header);
    const search = findElement(
      header?.props?.right,
      (props) => props.accessibilityLabel === 'Search questions',
    );
    const glyph = findElementByType(search?.props?.children, MagnifyingGlass);

    expect(glyph).toBeDefined();
    expect(glyph?.props?.color).toBe(colors.text.primary);
    expect(glyph?.props?.weight).toBe('bold');
  });

  it('opens the list’s own search instead of the app-wide overlay', () => {
    const header = findElementByType(InquiryListScreen(PROPS), Header);
    const search = findElement(
      header?.props?.right,
      (props) => props.accessibilityLabel === 'Search questions',
    );

    expect(search?.props?.accessibilityState).toMatchObject({ expanded: false });
    // The control is a toggle over this screen's state, so pressing it writes the
    // open flag rather than handing the reader to another screen (KNOT-020k-fix11).
    (search?.props?.onPress as () => void)();
    const writes = mockStateSetters.flatMap((setter) => setter.mock.calls as unknown[][]);
    expect(writes.some(([value]) => value === true)).toBe(true);
  });

  it('opens the signed-in person profile from the header avatar', () => {
    const onOpenProfile = jest.fn();
    const header = findElementByType(InquiryListScreen({ ...PROPS, onOpenProfile }), Header);
    const avatar = findElementByType(header?.props?.right, Avatar);

    expect(avatar?.props?.displayName).toBe('Nomsa Khan');
    (avatar?.props?.onPress as () => void)();

    expect(onOpenProfile).toHaveBeenCalledTimes(1);
  });
});

describe('InquiryListScreen inline search', () => {
  it('stays closed until the bar’s magnifier is used', () => {
    const hero = heroFor(InquiryListScreen(PROPS));

    expect(findByTestID(hero, 'inquiries-search-row')).toBeUndefined();
    expect(findByTestID(hero, 'inquiries-search-input')).toBeUndefined();
  });

  it('reveals a question search under the bar when it is opened', () => {
    scriptState({ 6: true });
    const hero = heroFor(InquiryListScreen(PROPS));
    const field = findByTestID(hero, 'inquiries-search-input');

    expect(findByTestID(hero, 'inquiries-search-row')).toBeDefined();
    expect(field).toBeDefined();
    expect(field?.props?.placeholder).toBe('Search questions');
    expect(field?.props?.accessibilityLabel).toBe('Search questions');
  });

  it('narrows the questions to the typed phrase', () => {
    scriptState({
      0: [
        inquiryFixture('one', 'Why red ribbons?', 'At the festival.', 'Manguzi'),
        inquiryFixture('two', 'Who keeps the drums?', 'The elders do.', 'Durban'),
      ],
      6: true,
      7: 'ribbons',
    });

    const tree = InquiryListScreen(PROPS);
    const data = findElementByType(tree, 'FlatList')?.props?.data as readonly Inquiry[];

    expect(data.map((inquiry) => inquiry.id)).toEqual(['one']);
  });

  it('names the empty state when nothing matches', () => {
    // `loading` is slot 2: the first render is loading, and an empty state is only
    // owed once the page has been read.
    scriptState({ 2: false, 6: true, 7: 'nothing matches this' });
    const list = findElementByType(InquiryListScreen(PROPS), 'FlatList');
    const empty = list?.props?.ListEmptyComponent as ElementLike | null | undefined;

    expect(empty).not.toBeNull();
    expect(findElementByType(empty, EmptyState)?.props?.headline).toBe(
      "No questions match 'nothing matches this'.",
    );
  });
});

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

  it('draws the hero line as an outlined button, not a filled field', () => {
    const onAsk = jest.fn();
    const hero = heroFor(InquiryListScreen({ ...PROPS, onAsk }));
    const line = findByTestID(hero, 'inquiries-place-pill');
    const prompt = findByTestID(hero, 'inquiries-ask-prompt');
    const ask = findElementByType(line, Button);

    // Transparent with a hairline outline and a soft corner: a button that starts
    // something, not the filled search field it used to be and not a full pill
    // (KNOT-020k-fix13).
    expect(line?.props?.style).toMatchObject({
      alignItems: 'center',
      backgroundColor: 'transparent',
      borderColor: colors.border.hairline,
      borderRadius: radius.lg,
      borderWidth: 1,
      flexDirection: 'row',
    });

    // The pin and the invitation are the prompt's content…
    expect(findElementByType(prompt, MapPin)).toBeDefined();
    expect(findText(prompt, 'Ask about a place, a tradition, or a word...')).toBeDefined();
    // …which stretches across the line's left side…
    expect(prompt?.props?.style).toMatchObject({ flex: 1 });
    // …and nothing on the hero accepts typing any more.
    expect(findElementByType(hero, 'TextInput')).toBeUndefined();

    // The Ask button is unchanged apart from its cap: primary, with an 8pt corner
    // that sits concentric inside the 12pt line (KNOT-020k-fix14).
    expect(ask).toBeDefined();
    expect(ask?.props?.label).toBe('Ask');
    expect(ask?.props?.variant).toBe('primary');
    expect(ask?.props?.shape).toBe('rounded');

    // Both halves of the line open the composer, and neither carries a place into
    // it (KNOT-020k-fix13).
    (prompt?.props?.onPress as () => void)();
    (ask?.props?.onPress as () => void)();
    expect(onAsk).toHaveBeenCalledTimes(2);
    expect(onAsk).toHaveBeenCalledWith();
  });

  it('keeps the bar’s magnifier as the only search icon on the screen', () => {
    // Closed: the hero draws no magnifier at all.
    expect(findElementByType(heroFor(InquiryListScreen(PROPS)), MagnifyingGlass)).toBeUndefined();

    // Open: the search field is a plain field, so the bar's control is still the
    // only mark that means "search" (KNOT-020k-fix12).
    scriptState({ 6: true });
    const openHero = heroFor(InquiryListScreen(PROPS));

    expect(findByTestID(openHero, 'inquiries-search-input')).toBeDefined();
    expect(findElementByType(openHero, MagnifyingGlass)).toBeUndefined();
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

  it('offers the starters alone when the hero is at rest', () => {
    // With nothing typed and no place filter left to drive, the chips are the
    // starters alone (KNOT-020k-fix13).
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

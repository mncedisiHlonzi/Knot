import ReactionBar from '../ReactionBar';
import { colors, typeScale } from '../../theme';

// ReactionBar is stateful, so the project's renderer-less pattern stubs the hook
// primitives and calls the component as a plain function. The initial state is the
// server copy the caller passes, which is what a static call can honestly assert.
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useCallback: (callback: unknown) => callback,
  useEffect: () => undefined,
  useRef: (initial: unknown) => ({ current: initial }),
  // Call a lazy initializer, as React would, so state backed by a function (the
  // seeded reactions list) is the value rather than the function itself.
  useState: (initial: unknown) => [
    typeof initial === 'function' ? (initial as () => unknown)() : initial,
    () => undefined,
  ],
}));

jest.mock('react-native', () => ({
  Alert: { alert: jest.fn() },
  // The pills pulse on tap, which is visual only; the stand-ins exist so a direct
  // call renders (KNOT-020h).
  Animated: {
    Value: class {
      setValue(): void {}
    },
    createAnimatedComponent: <T>(component: T): T => component,
    sequence: () => ({ start: () => undefined, stop: () => undefined }),
    timing: () => ({ start: () => undefined, stop: () => undefined }),
  },
  Easing: { bezier: () => (progress: number) => progress },
  Pressable: 'Pressable',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

// The reactions client pulls in the API base URL module, which imports the
// gitignored `secrets.local` file in CI.
jest.mock('../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));

/** Collects every string child in a directly-called tree. */
function collectText(node: unknown, found: string[] = []): string[] {
  if (Array.isArray(node)) {
    for (const child of node) {
      collectText(child, found);
    }
    return found;
  }
  if (typeof node === 'string') {
    found.push(node);
    return found;
  }
  if (typeof node === 'number') {
    found.push(String(node));
    return found;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return found;
  }

  const element = node as { props?: { children?: unknown } };
  if (element.props !== undefined) {
    collectText(element.props.children, found);
  }

  return found;
}

/** Finds the first element whose props satisfy the predicate. */
function findElement(
  node: unknown,
  predicate: (props: Record<string, unknown>) => boolean,
): { props?: Record<string, unknown> } | undefined {
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

  const element = node as { props?: Record<string, unknown>; children?: unknown };
  if (element.props !== undefined && predicate(element.props)) {
    return element;
  }
  return findElement(element.props?.children, predicate);
}

const EMPTY = {
  rings_true: 0,
  know_it_differently: 0,
  adds_something_new: 0,
  needs_a_source: 0,
};

function render(overrides: {
  readonly myReactions?: readonly string[];
  readonly counts?: typeof EMPTY;
}) {
  return ReactionBar({
    entityType: 'story',
    entityId: 'story-1',
    counts: overrides.counts ?? EMPTY,
    myReactions: overrides.myReactions ?? [],
    token: undefined,
    onChange: () => undefined,
    layout: 'grid',
  });
}

describe('ReactionBar', () => {
  it('renders the four signals as text pills with no glyphs', () => {
    const text = collectText(render({}));

    expect(text).toContain('Rings true');
    expect(text).toContain('Know it differently');
    expect(text).toContain('Adds something new');
    expect(text).toContain('Needs a source');
  });

  it('keeps every grid label on one line so the pills stay tidy', () => {
    const element = render({});
    const label = findElement(element, (props) => props.children === 'Know it differently');

    expect(label?.props?.numberOfLines).toBe(1);
  });

  it('uses a bordered paper pill with the label type, held to 36pt in the grid', () => {
    const element = render({});
    const pill = findElement(
      element,
      (props) =>
        typeof props.accessibilityLabel === 'string' &&
        props.accessibilityLabel.startsWith('Rings true'),
    );
    const styles = Array.isArray(pill?.props?.style)
      ? (pill?.props?.style as Array<Record<string, unknown>>)
      : [];

    expect(styles.some((style) => style?.backgroundColor === colors.bg.paper)).toBe(true);
    expect(styles.some((style) => style?.borderWidth === 1)).toBe(true);
    expect(styles.some((style) => style?.height === 36)).toBe(true);

    const label = findElement(element, (props) => props.children === 'Rings true');
    expect(label?.props?.style).toEqual(expect.arrayContaining([typeScale.label]));
  });

  it('fills a held signal with the brand tint', () => {
    const element = render({ myReactions: ['rings_true'] });
    const pill = findElement(
      element,
      (props) =>
        typeof props.accessibilityLabel === 'string' &&
        props.accessibilityLabel.startsWith('Rings true'),
    );

    const styles = Array.isArray(pill?.props?.style)
      ? (pill?.props?.style as Array<Record<string, unknown>>)
      : [];
    expect(styles.some((style) => style?.backgroundColor === colors.brand.tint)).toBe(true);
    expect(pill?.props?.accessibilityState).toEqual({ selected: true });
  });

  it('shows a held signal count inside its pill', () => {
    const text = collectText(
      render({ myReactions: ['rings_true'], counts: { ...EMPTY, rings_true: 128 } }),
    );

    expect(text).toContain('128');
  });
});

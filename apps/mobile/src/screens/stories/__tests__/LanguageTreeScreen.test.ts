import TheThread from '../../../components/TheThread';
import LanguageTreeScreen from '../LanguageTreeScreen';

/**
 * The Language Tree's loading state must mount the loading line with the
 * `tree-thread` testID, drawn HORIZONTALLY (KNOT-033). The screen holds state and
 * fetches on mount, so the project's renderer-less pattern applies: React's hook
 * primitives are stubbed and the state is scripted.
 *
 * The hook order is fixed — versions, loading, error — so the script below is
 * positional: an empty tree, `loading` as the test asks, no error.
 */
let mockStateValues: unknown[] = [];
let mockStateCursor = 0;

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useCallback: (callback: unknown) => callback,
  useEffect: () => undefined,
  // The helpers are pure, so running them is what the screen would do anyway.
  useMemo: (factory: () => unknown) => factory(),
  useState: (initial: unknown) => {
    const value = mockStateValues[mockStateCursor];
    mockStateCursor += 1;
    return [value === undefined ? initial : value, () => undefined];
  },
}));

jest.mock('react-native', () => ({
  Image: 'Image',
  Pressable: 'Pressable',
  ScrollView: 'ScrollView',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

// Only the icon the screen imports directly; the components it renders while
// loading draw none.
jest.mock('phosphor-react-native', () => ({ ArrowLeft: () => null }));

// The API client imports the gitignored `secrets.local` through this module.
jest.mock('../../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));

type ElementLike = { type?: unknown; props?: Record<string, unknown> };

/** Finds the first element carrying a testID. */
function findByTestId(node: unknown, testID: string): ElementLike | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findByTestId(child, testID);
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
  return findByTestId(element.props?.children, testID);
}

/** Calls the screen directly with the scripted loading state. */
function render(loading: boolean): unknown {
  mockStateValues = [[], loading, undefined];
  mockStateCursor = 0;

  return LanguageTreeScreen({
    storyId: 'story-1',
    onOpenUserProfile: () => undefined,
    onOpenVersion: () => undefined,
    onBack: () => undefined,
  });
}

describe('LanguageTreeScreen', () => {
  it('mounts the horizontal Thread above the rows while the tree loads', () => {
    const thread = findByTestId(render(true), 'tree-thread');

    // The testID sits on the component itself, so finding it proves the loading
    // state renders the Thread rather than a bare placeholder.
    expect(thread?.type).toBe(TheThread);
    // `orientation` is not passed: the Thread's default is horizontal, and
    // resolving it here keeps a vertical draw from returning unnoticed
    // (KNOT-033).
    expect((thread?.props?.orientation as string | undefined) ?? 'horizontal').toBe('horizontal');
    expect(thread?.props?.length).toBe(112);
  });

  it('draws no Thread once the tree has loaded', () => {
    expect(findByTestId(render(false), 'tree-thread')).toBeUndefined();
  });
});

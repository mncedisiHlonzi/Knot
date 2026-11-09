import { storiesApi } from '../../../api/stories';
import EditStoryScreen, { validateStoryEdit } from '../EditStoryScreen';

/**
 * The Edit Story screen reads its draft from the server and writes it back with
 * `PUT /stories/{id}` (KNOT-054).
 *
 * There is no renderer in this project (JEST is `testEnvironment: 'node'`), so the
 * screen is called as a plain function with React's hook primitives stubbed, the
 * pattern the other stateful screens use: `useEffect` never runs (the scripted
 * state below IS the loaded state), `useCallback` returns its callback unchanged,
 * and `useState` is fed a positional sequence while recording its setters.
 *
 * The sequence is: title, body, loading, saving, error.
 */
const mockStateValues: unknown[] = [];
const mockStateSetters: jest.Mock[] = [];
let mockStateCursor = 0;

jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useEffect: () => undefined,
  useRef: (initial: unknown) => ({ current: initial }),
  useState: (initial: unknown) => {
    const value = mockStateValues[mockStateCursor];
    mockStateCursor += 1;
    const setter = jest.fn();
    mockStateSetters.push(setter);
    return [value === undefined ? initial : value, setter];
  },
}));

jest.mock('react-native', () => ({
  ActivityIndicator: 'ActivityIndicator',
  Pressable: 'Pressable',
  ScrollView: 'ScrollView',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  TextInput: 'TextInput',
  View: 'View',
}));

jest.mock('phosphor-react-native', () => ({ X: () => null }));

jest.mock('../../../api/client', () => ({ describeError: () => 'request failed' }));
jest.mock('../../../api/stories', () => ({
  storiesApi: { getStory: jest.fn(), updateStory: jest.fn() },
}));

type ElementLike = { type?: unknown; props?: Record<string, unknown> };

/**
 * Finds the first element carrying the given testID.
 *
 * The header's controls live in its `left`/`right` props rather than in
 * `children`, so the walk visits every prop value, not only the children.
 */
function findByTestID(node: unknown, testID: string): ElementLike | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findByTestID(child, testID);
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

  for (const value of Object.values(element.props ?? {})) {
    const found = findByTestID(value, testID);
    if (found !== undefined) {
      return found;
    }
  }
  return undefined;
}

/** One render with the scripted state, exactly as the app builds it. */
function renderScreen(values: readonly unknown[], onSaved = jest.fn()): unknown {
  mockStateValues.length = 0;
  mockStateValues.push(...values);
  mockStateCursor = 0;
  mockStateSetters.length = 0;

  return EditStoryScreen({
    storyId: 'story-1',
    token: 'token',
    onSaved,
    onCancel: () => undefined,
  });
}

describe('validateStoryEdit', () => {
  it('accepts a title and a body', () => {
    expect(validateStoryEdit('A title', 'A body.')).toBeUndefined();
  });

  it('rejects a blank title', () => {
    expect(validateStoryEdit('   ', 'A body.')).toBe('A title is required.');
  });

  it('rejects a title over 200 characters', () => {
    expect(validateStoryEdit('a'.repeat(201), 'A body.')).toBe(
      'The title must be at most 200 characters.',
    );
  });

  it('rejects a blank body', () => {
    expect(validateStoryEdit('A title', '  \n ')).toBe('A story is required.');
  });

  it('rejects a body over 10,000 characters', () => {
    expect(validateStoryEdit('A title', 'a'.repeat(10001))).toBe(
      'The story must be at most 10000 characters.',
    );
  });
});

describe('EditStoryScreen', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('opens with the story’s current text in its two fields', () => {
    const tree = renderScreen(['The original title', 'The original body.', false, false]);

    const title = findByTestID(tree, 'edit-title');
    const body = findByTestID(tree, 'edit-body');

    expect(title?.props?.value).toBe('The original title');
    expect(body?.props?.value).toBe('The original body.');
  });

  it('saves the trimmed title and the body verbatim', async () => {
    (storiesApi.updateStory as jest.Mock).mockResolvedValue({
      story: { id: 'story-1', title: 'A new title', body: 'A new body.' },
    });
    const onSaved = jest.fn();
    const tree = renderScreen(['  A new title  ', 'A new body.', false, false], onSaved);

    (findByTestID(tree, 'edit-save')?.props?.onPress as () => void)();
    await Promise.resolve();

    expect(storiesApi.updateStory).toHaveBeenCalledWith('story-1', 'token', {
      title: 'A new title',
      body: 'A new body.',
    });
    expect(onSaved).toHaveBeenCalledWith({
      id: 'story-1',
      title: 'A new title',
      body: 'A new body.',
    });
  });

  it('refuses an empty draft without calling the server', () => {
    const tree = renderScreen(['   ', 'A body.', false, false]);

    (findByTestID(tree, 'edit-save')?.props?.onPress as () => void)();

    expect(storiesApi.updateStory).not.toHaveBeenCalled();
    const writes = mockStateSetters.flatMap((setter) => setter.mock.calls as unknown[][]);
    expect(writes.some(([value]) => value === 'A title is required.')).toBe(true);
  });

  it('reports a refused save and stays open', async () => {
    (storiesApi.updateStory as jest.Mock).mockRejectedValue(new Error('offline'));
    const onSaved = jest.fn();
    const tree = renderScreen(['A title', 'A body.', false, false], onSaved);

    (findByTestID(tree, 'edit-save')?.props?.onPress as () => void)();
    await Promise.resolve();
    await Promise.resolve();

    expect(onSaved).not.toHaveBeenCalled();
    const writes = mockStateSetters.flatMap((setter) => setter.mock.calls as unknown[][]);
    expect(writes.some(([value]) => value === 'request failed')).toBe(true);
  });

  it('draws a spinner instead of the fields while the draft loads', () => {
    const tree = renderScreen(['', '', true, false]);

    expect(findByTestID(tree, 'edit-title')).toBeUndefined();
    expect(findByTestID(tree, 'edit-save')).toBeDefined();
  });

  it('leaves without saving when the close control is used', () => {
    const onCancel = jest.fn();
    mockStateValues.length = 0;
    mockStateValues.push('A title', 'A body.', false, false);
    mockStateCursor = 0;
    mockStateSetters.length = 0;

    const tree = EditStoryScreen({
      storyId: 'story-1',
      token: 'token',
      onSaved: jest.fn(),
      onCancel,
    });

    (findByTestID(tree, 'edit-close')?.props?.onPress as () => void)();

    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(storiesApi.updateStory).not.toHaveBeenCalled();
  });
});

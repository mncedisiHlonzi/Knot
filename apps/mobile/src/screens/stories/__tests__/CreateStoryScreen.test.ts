import { Alert } from 'react-native';

import Button from '../../../components/Button';
import Header from '../../../components/Header';
import LanguagePicker from '../../../components/LanguagePicker';
import LocationPicker from '../../../components/LocationPicker';
import { MAX_MEDIA_PER_STORY } from '../../../utils/mediaLimit';
import { colors, typeScale } from '../../../theme';
import { storiesApi, uploadStoryMedia } from '../../../api/stories';
import CreateStoryScreen from '../CreateStoryScreen';

/**
 * The composer is media-first and text-first at once (KNOT-020k-fix14): a bar with
 * close and Publish, a metadata row, two borderless fields, the media strip and a
 * footer. This project has no renderer and must not add one, so the screen is
 * called as a plain function with React's hook primitives stubbed: `useState`
 * returns the initial value unless a test scripts that slot with `scriptState`. The
 * pickers, the media sheet and the API are stubbed, so the element tree holds
 * their elements without invoking them.
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
  // The publish notice is a native alert (KNOT-037-fix12).
  Alert: { alert: jest.fn() },
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
  Image: 'Image',
  Pressable: 'Pressable',
  ScrollView: 'ScrollView',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  TextInput: 'TextInput',
  View: 'View',
}));

jest.mock('phosphor-react-native', () => ({
  FilmStrip: () => null,
  Flag: () => null,
  Globe: () => null,
  ImageSquare: () => null,
  MapPin: () => null,
  Plus: () => null,
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
jest.mock('../../../components/MediaPickerSheet', () => ({
  __esModule: true,
  default: 'MediaPickerSheet',
}));
jest.mock('../../../components/CameraCaptureBadge', () => ({
  __esModule: true,
  default: 'CameraCaptureBadge',
}));

jest.mock('../../../api/client', () => ({ describeError: () => 'request failed' }));
jest.mock('../../../api/stories', () => ({
  PILLARS: ['wonder', 'heritage'],
  storiesApi: { createStory: jest.fn() },
  uploadStoryMedia: jest.fn(),
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
 * Scripts the stubbed `useState` by slot index, in hook order: pillar 0, title 1,
 * body 2, language 3, locationText 4, selectedPlace 5, error 6, submitting 7,
 * **media 8**, pickerVisible 9, **openPicker 10**, mediaNotice 11.
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

/** One picked image, for the strip. */
function pickedImage(uri: string): {
  uri: string;
  type: 'image';
  mimeType: string;
  size: number;
  source: 'library';
} {
  return { uri, type: 'image', mimeType: 'image/jpeg', size: 1024, source: 'library' };
}

const PROPS = {
  token: 'token',
  onCreated: () => undefined,
  onCancel: () => undefined,
};

beforeEach(() => {
  mockStateValues.length = 0;
  mockStateSetters.length = 0;
});

describe('CreateStoryScreen bar', () => {
  it('carries only a close control and the Publish action', () => {
    const header = findElementByType(CreateStoryScreen(PROPS), Header);
    const close = findElement(
      header?.props?.left,
      (props) => props.accessibilityLabel === 'Close the composer',
    );
    const publish = findElementByType(header?.props?.right, Button);

    expect(header?.props?.title).toBeUndefined();
    expect(close).toBeDefined();
    expect(publish?.props?.label).toBe('Publish');
    expect(publish?.props?.variant).toBe('primary');
  });

  it('closes the composer from the bar', () => {
    const onCancel = jest.fn();
    const header = findElementByType(CreateStoryScreen({ ...PROPS, onCancel }), Header);
    const close = findElement(
      header?.props?.left,
      (props) => props.accessibilityLabel === 'Close the composer',
    );

    (close?.props?.onPress as () => void)();

    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});

describe('CreateStoryScreen metadata', () => {
  it('offers the pillar pair, the language and the place in one row', () => {
    const tree = CreateStoryScreen(PROPS);

    expect(findByTestID(tree, 'create-pillar-wonder')?.props?.selected).toBe(true);
    expect(findByTestID(tree, 'create-pillar-heritage')?.props?.selected).toBe(false);

    const language = findByTestID(tree, 'create-language-chip');
    expect(language?.props?.label).toBe('English');
    expect(language?.props?.variant).toBe('outline');
    expect(language?.props?.icon).toBeDefined();

    expect(findByTestID(tree, 'create-place-chip')?.props?.label).toBe('Place');
  });

  it('draws the selected pillar from the state the chips set', () => {
    scriptState({ 0: 'heritage' });
    const tree = CreateStoryScreen(PROPS);

    expect(findByTestID(tree, 'create-pillar-heritage')?.props?.selected).toBe(true);
    expect(findByTestID(tree, 'create-pillar-wonder')?.props?.selected).toBe(false);
  });

  it('opens the picker a chip names', () => {
    scriptState({ 10: 'language' });
    const tree = CreateStoryScreen(PROPS);

    expect(findElementByType(tree, LanguagePicker)).toBeDefined();
    expect(findElementByType(tree, LocationPicker)).toBeUndefined();
  });

  it('records the picker a tapped chip asks for', () => {
    const tree = CreateStoryScreen(PROPS);

    (findByTestID(tree, 'create-place-chip')?.props?.onPress as () => void)();

    expect(stateWrites()).toContain('place');
  });
});

describe('CreateStoryScreen fields', () => {
  it('draws the headline and the story borderlessly at their own steps', () => {
    const tree = CreateStoryScreen(PROPS);
    const title = findByTestID(tree, 'create-title');
    const body = findByTestID(tree, 'create-body');
    const titleStyle = title?.props?.style as readonly unknown[];

    expect(titleStyle?.[0]).toBe(typeScale.h1);
    expect(titleStyle?.[1]).not.toHaveProperty('borderBottomWidth');
    expect(titleStyle?.[1]).not.toHaveProperty('borderWidth');

    expect((body?.props?.style as readonly unknown[])[0]).toBe(typeScale.reading);
    expect(body?.props?.multiline).toBe(true);
  });

  it('reports a missing title instead of publishing', () => {
    // The action lives in the bar's trailing slot, which is a prop rather than a
    // child, so it is read from the bar itself.
    const header = findElementByType(CreateStoryScreen(PROPS), Header);
    const publish = findElementByType(header?.props?.right, Button);

    (publish?.props?.onPress as () => void)();

    expect(stateWrites()).toContain('A title is required.');
  });
});

describe('CreateStoryScreen media strip', () => {
  it('counts the items against the server cap', () => {
    const tree = CreateStoryScreen(PROPS);
    const label = findByTestID(tree, 'create-media-label');

    expect(label?.props?.children).toBe(`Media · 0 of ${MAX_MEDIA_PER_STORY}`);
  });

  it('counts what the strip actually holds', () => {
    scriptState({ 8: [pickedImage('file:///a.jpg'), pickedImage('file:///b.jpg')] });
    const tree = CreateStoryScreen(PROPS);

    expect(findByTestID(tree, 'create-media-label')?.props?.children).toBe(
      `Media · 2 of ${MAX_MEDIA_PER_STORY}`,
    );
  });

  it('offers a working add slot while there is room, and closes it at the cap', () => {
    const open = findByTestID(CreateStoryScreen(PROPS), 'create-add-media');
    expect(open?.props?.disabled).toBe(false);

    scriptState({
      8: Array.from({ length: MAX_MEDIA_PER_STORY }, (_, index) =>
        pickedImage(`file:///${index}.jpg`),
      ),
    });
    const full = findByTestID(CreateStoryScreen(PROPS), 'create-add-media');

    expect(full?.props?.disabled).toBe(true);
    expect((full?.props?.style as readonly unknown[])[1]).toMatchObject({ opacity: 0.5 });
  });
});

describe('CreateStoryScreen footer', () => {
  it('carries the three quick marks and an honest status', () => {
    const footer = findByTestID(CreateStoryScreen(PROPS), 'create-footer');

    expect(findByTestID(footer, 'create-footer-media')).toBeDefined();
    expect(findByTestID(footer, 'create-footer-place')).toBeDefined();
    expect(findByTestID(footer, 'create-footer-pillar')).toBeDefined();

    // Nothing is persisted until Publish succeeds, so the design's "Draft saved"
    // would be a promise the app cannot keep (KNOT-020k-fix14).
    const status = findText(footer, 'Not published yet');
    expect(status).toBeDefined();
    expect((status?.props?.style as readonly unknown[])[1]).toMatchObject({
      color: colors.text.tertiary,
    });
  });

  it('flips the pillar from the flag', () => {
    const tree = CreateStoryScreen(PROPS);

    (findByTestID(tree, 'create-footer-pillar')?.props?.onPress as () => void)();

    // A state updater, because the flip reads the previous value.
    expect(stateWrites().some((value) => typeof value === 'function')).toBe(true);
  });
});

describe('CreateStoryScreen publish', () => {
  /** The Publish action, which lives in the bar's trailing prop. */
  function publishButton(tree: unknown): ElementLike | undefined {
    const header = findElementByType(tree, Header);
    return findElementByType(header?.props?.right, Button);
  }

  /** A story as `POST /stories` returns it: stored, with no media rows yet. */
  function createdStory(): { id: string; [key: string]: unknown } {
    return {
      id: 'story-1',
      author_id: 'user-1',
      root_version_id: 'version-1',
      pillar: 'wonder',
      language: 'eng',
      title: 'A title',
      body: 'A body',
      approximate_location: '',
      media_urls: [],
      created_at: '2026-11-02T08:00:00.000Z',
      updated_at: '2026-11-02T08:00:00.000Z',
    };
  }

  /** Taps Publish and lets the awaited story creation settle. */
  async function publish(tree: unknown): Promise<void> {
    (publishButton(tree)?.props?.onPress as () => void)();
    await Promise.resolve();
    await Promise.resolve();
  }

  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('tells the reader the media is still being processed', async () => {
    (storiesApi.createStory as jest.Mock).mockResolvedValue({ story: createdStory() });
    (uploadStoryMedia as jest.Mock).mockResolvedValue(undefined);

    scriptState({ 1: 'A title', 2: 'A body' });
    await publish(CreateStoryScreen(PROPS));

    expect(Alert.alert).toHaveBeenCalledWith(
      'Story published',
      expect.stringContaining('being processed'),
    );
  });

  it('hands the story over before the media upload has finished', async () => {
    (storiesApi.createStory as jest.Mock).mockResolvedValue({ story: createdStory() });
    // The upload never settles: the handoff must not be waiting for it.
    (uploadStoryMedia as jest.Mock).mockReturnValue(new Promise(() => undefined));

    const onCreated = jest.fn();
    scriptState({ 1: 'A title', 2: 'A body', 8: [pickedImage('file:///a.jpg')] });
    await publish(CreateStoryScreen({ ...PROPS, onCreated }));

    expect(onCreated).toHaveBeenCalledTimes(1);
    const handed = onCreated.mock.calls[0][0] as {
      media?: readonly { status?: string }[];
    };
    expect(handed.media?.[0]?.status).toBe('processing');
    expect(uploadStoryMedia).toHaveBeenCalledTimes(1);
  });

  it('keeps the composer open when the story itself cannot be created', async () => {
    (storiesApi.createStory as jest.Mock).mockRejectedValue(new Error('offline'));
    (uploadStoryMedia as jest.Mock).mockResolvedValue(undefined);

    const onCreated = jest.fn();
    scriptState({ 1: 'A title', 2: 'A body', 8: [pickedImage('file:///a.jpg')] });
    await publish(CreateStoryScreen({ ...PROPS, onCreated }));

    expect(onCreated).not.toHaveBeenCalled();
    expect(uploadStoryMedia).not.toHaveBeenCalled();
    expect(stateWrites()).toContain('request failed');
  });

  it('reports the direct upload of each item as it starts', async () => {
    (storiesApi.createStory as jest.Mock).mockResolvedValue({ story: createdStory() });
    // Never settles: the first progress report must not wait for the bytes.
    (uploadStoryMedia as jest.Mock).mockReturnValue(new Promise(() => undefined));

    const onUploadProgress = jest.fn();
    scriptState({
      1: 'A title',
      2: 'A body',
      8: [pickedImage('file:///a.jpg'), pickedImage('file:///b.jpg')],
    });
    await publish(CreateStoryScreen({ ...PROPS, onUploadProgress }));

    // Two files: the card is told "1 of 2" before the first byte moves (KNOT-040).
    expect(onUploadProgress).toHaveBeenCalledWith(createdStory().id, {
      completed: 0,
      total: 2,
      failed: false,
    });
  });

  it('tells the reader when an upload fails, and keeps the story', async () => {
    (storiesApi.createStory as jest.Mock).mockResolvedValue({ story: createdStory() });
    (uploadStoryMedia as jest.Mock).mockRejectedValue(new Error('storage refused'));

    const onUploadProgress = jest.fn();
    scriptState({ 1: 'A title', 2: 'A body', 8: [pickedImage('file:///a.jpg')] });
    await publish(CreateStoryScreen({ ...PROPS, onUploadProgress }));

    // The upload runs after the handoff, so give its promise chain room to finish.
    for (let step = 0; step < 8; step += 1) {
      await Promise.resolve();
    }

    expect(onUploadProgress).toHaveBeenLastCalledWith(createdStory().id, {
      completed: 1,
      total: 1,
      failed: true,
    });
    expect(Alert.alert).toHaveBeenCalledWith('Media upload failed', 'request failed');
  });
});

import { Alert, Platform } from 'react-native';
import { launchCamera, launchImageLibrary } from 'react-native-image-picker';

import MediaPickerSheet from '../MediaPickerSheet';
import { MEDIA_LIMIT_MESSAGE } from '../../utils/mediaLimit';

/**
 * The media sheet's two doors (KNOT-052).
 *
 * It used to ask the reader four questions — take a photo, record a clip, choose
 * a photo, choose a clip — which made them name the same thing twice. Now it asks
 * only where the media comes from, and the native picker decides the kind: `mixed`
 * for both doors, and each asset is classified as it arrives.
 *
 * There is no renderer in this project, so the component is called as a plain
 * function with React's hook primitives stubbed and the native pickers mocked.
 */
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useCallback: (callback: unknown) => callback,
  useMemo: (factory: () => unknown) => factory(),
  useRef: (initial: unknown) => ({ current: initial }),
  useEffect: () => undefined,
}));

jest.mock('react-native', () => ({
  Alert: { alert: jest.fn() },
  Animated: {
    View: 'Animated.View',
    Value: class {
      setValue(): void {}
    },
    spring: () => ({ start: () => undefined }),
  },
  Modal: 'Modal',
  PanResponder: { create: () => ({ panHandlers: {} }) },
  Platform: { OS: 'ios' },
  Pressable: 'Pressable',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

jest.mock('react-native-image-picker', () => ({
  launchCamera: jest.fn(),
  launchImageLibrary: jest.fn(),
}));

jest.mock('phosphor-react-native', () => ({
  Camera: () => null,
  Images: () => null,
}));

jest.mock('../../motion/useFade', () => ({
  useFade: () => ({ opacity: 0, fadeIn: () => undefined }),
}));

jest.mock('../../theme', () => ({
  spacing: { xs: 4, sm: 8, md: 12, lg: 16, xl: 24 },
  radius: { lg: 12, xxl: 24 },
  elevation: [{}, {}, {}, {}],
  fontWeights: { bold: '700' },
  typeScale: { body: {}, button: {}, caption: {} },
  useTheme: () => ({ colors: { text: { primary: '#000000', tertiary: '#767676' } } }),
  useThemedStyles: () => ({}),
}));

type Element = { type?: unknown; props?: Record<string, unknown> };

/** Every element in a directly-called component's tree, depth-first. */
function elements(node: unknown, found: Element[] = []): Element[] {
  if (Array.isArray(node)) {
    for (const child of node) {
      elements(child, found);
    }
    return found;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return found;
  }

  const element = node as Element;
  if (element.type !== undefined) {
    found.push(element);
  }
  if (element.props !== undefined) {
    elements(element.props.children, found);
  }
  return found;
}

/** The choice carrying this identifier, or undefined when the sheet drew none. */
function option(tree: unknown, testID: string): Element | undefined {
  return elements(tree).find((element) => element.props?.testID === testID);
}

/** The mocked platform, so a test can choose what the sheet thinks it is on. */
const platform = Platform as unknown as { OS: string };

const PROPS = {
  visible: true,
  onClose: jest.fn(),
  onPicked: jest.fn(),
  onError: jest.fn(),
};

beforeEach(() => {
  jest.clearAllMocks();
  // iOS by default: the camera's own mode control decides the kind there.
  platform.OS = 'ios';
  (launchCamera as jest.Mock).mockResolvedValue({ didCancel: true });
  (launchImageLibrary as jest.Mock).mockResolvedValue({ didCancel: true });
});

describe('the sheet offers two doors', () => {
  it('draws a camera and a gallery, and nothing else', () => {
    const tree = MediaPickerSheet(PROPS);
    const testIDs = elements(tree)
      .map((element) => element.props?.testID)
      .filter((testID): testID is string => typeof testID === 'string');

    // The four old rows are gone: two tiles are the whole choice.
    expect(testIDs).toEqual(['media-picker-camera', 'media-picker-gallery']);
  });

  it('names both kinds on each door when clips are allowed', () => {
    const tree = MediaPickerSheet(PROPS);

    expect(option(tree, 'media-picker-camera')?.props?.label).toBe('Camera');
    expect(option(tree, 'media-picker-camera')?.props?.hint).toBe('Photo or video');
    expect(option(tree, 'media-picker-gallery')?.props?.label).toBe('Gallery');
    expect(option(tree, 'media-picker-gallery')?.props?.hint).toBe('Photos and videos');
  });

  it('says only "A photo" when the caller wants an avatar', () => {
    const tree = MediaPickerSheet({ ...PROPS, allowVideo: false });

    expect(option(tree, 'media-picker-camera')?.props?.hint).toBe('A photo');
    expect(option(tree, 'media-picker-gallery')?.props?.hint).toBe('A photo');
  });
});

describe('the camera door', () => {
  it('asks the native camera for photographs and clips alike on iOS', () => {
    const tree = MediaPickerSheet(PROPS);

    (option(tree, 'media-picker-camera')?.props?.onPress as () => void)();

    expect(launchCamera).toHaveBeenCalledWith(expect.objectContaining({ mediaType: 'mixed' }));
    expect(Alert.alert).not.toHaveBeenCalled();
  });

  /**
   * Android's picker cannot offer both kinds in one camera, so the sheet asks
   * before the intent is built rather than silently taking a photograph
   * (KNOT-053).
   */
  it('asks which kind it is on Android, and launches nothing until the answer', () => {
    platform.OS = 'android';
    const tree = MediaPickerSheet(PROPS);

    (option(tree, 'media-picker-camera')?.props?.onPress as () => void)();

    expect(launchCamera).not.toHaveBeenCalled();
    expect(Alert.alert).toHaveBeenCalledTimes(1);

    const [, , buttons] = (Alert.alert as jest.Mock).mock.calls[0] as [
      string,
      string,
      { text: string; onPress?: () => void }[],
    ];
    expect(buttons.map((button) => button.text)).toEqual(['Take Photo', 'Record Video', 'Cancel']);

    buttons[0].onPress?.();
    expect(launchCamera).toHaveBeenLastCalledWith(expect.objectContaining({ mediaType: 'photo' }));

    buttons[1].onPress?.();
    expect(launchCamera).toHaveBeenLastCalledWith(expect.objectContaining({ mediaType: 'video' }));
  });

  it('asks the still camera directly when the caller allows no video', () => {
    platform.OS = 'android';
    const tree = MediaPickerSheet({ ...PROPS, allowVideo: false });

    (option(tree, 'media-picker-camera')?.props?.onPress as () => void)();

    // An avatar is an image: there is nothing to ask.
    expect(Alert.alert).not.toHaveBeenCalled();
    expect(launchCamera).toHaveBeenCalledWith(expect.objectContaining({ mediaType: 'photo' }));
  });

  it('reports a capture that could not open', async () => {
    (launchCamera as jest.Mock).mockRejectedValue(new Error('no camera'));
    const tree = MediaPickerSheet(PROPS);

    (option(tree, 'media-picker-camera')?.props?.onPress as () => void)();
    await Promise.resolve();
    await Promise.resolve();

    expect(PROPS.onError).toHaveBeenCalledWith('no camera');
  });
});

describe('the gallery door', () => {
  it('asks for photographs and clips, filling only the free slots', () => {
    const tree = MediaPickerSheet({ ...PROPS, maxSelection: 4 });

    (option(tree, 'media-picker-gallery')?.props?.onPress as () => void)();

    expect(launchImageLibrary).toHaveBeenCalledWith(
      expect.objectContaining({ mediaType: 'mixed', selectionLimit: 4 }),
    );
  });

  it('refuses a pick with no room instead of asking for an unlimited one', () => {
    const tree = MediaPickerSheet({ ...PROPS, maxSelection: 0 });

    (option(tree, 'media-picker-gallery')?.props?.onPress as () => void)();

    // A library treats a limit of 0 as "everything", which would be the bug.
    expect(launchImageLibrary).not.toHaveBeenCalled();
    expect(PROPS.onError).toHaveBeenCalledWith(MEDIA_LIMIT_MESSAGE);
    expect(PROPS.onClose).toHaveBeenCalled();
  });

  it('classifies a mixed selection item by item', async () => {
    (launchImageLibrary as jest.Mock).mockResolvedValue({
      assets: [
        { uri: 'file:///a.jpg', type: 'image/jpeg', fileSize: 100, width: 10, height: 20 },
        { uri: 'file:///b.mp4', type: 'video/mp4', fileSize: 900, duration: 5.5 },
      ],
    });
    const tree = MediaPickerSheet({ ...PROPS, maxSelection: 10 });

    (option(tree, 'media-picker-gallery')?.props?.onPress as () => void)();
    await Promise.resolve();
    await Promise.resolve();

    const picked = (PROPS.onPicked as jest.Mock).mock.calls[0][0] as readonly {
      readonly uri: string;
      readonly type: string;
      readonly mimeType: string;
      readonly source: string;
      readonly duration?: number;
    }[];

    expect(picked).toHaveLength(2);
    expect(picked[0]).toMatchObject({
      uri: 'file:///a.jpg',
      type: 'image',
      mimeType: 'image/jpeg',
      source: 'gallery',
    });
    expect(picked[1]).toMatchObject({
      uri: 'file:///b.mp4',
      type: 'video',
      mimeType: 'video/mp4',
      source: 'gallery',
      duration: 5.5,
    });
  });

  it('falls back to the duration when the asset reports no MIME type', async () => {
    (launchImageLibrary as jest.Mock).mockResolvedValue({
      assets: [{ uri: 'file:///clip', type: undefined, duration: 3 }],
    });
    const tree = MediaPickerSheet(PROPS);

    (option(tree, 'media-picker-gallery')?.props?.onPress as () => void)();
    await Promise.resolve();
    await Promise.resolve();

    const picked = (PROPS.onPicked as jest.Mock).mock.calls[0][0] as readonly {
      readonly type: string;
      readonly mimeType: string;
    }[];

    expect(picked[0].type).toBe('video');
    expect(picked[0].mimeType).toBe('video/mp4');
  });

  it('stays silent when the reader backs out', async () => {
    (launchImageLibrary as jest.Mock).mockResolvedValue({ didCancel: true });
    const tree = MediaPickerSheet(PROPS);

    (option(tree, 'media-picker-gallery')?.props?.onPress as () => void)();
    await Promise.resolve();
    await Promise.resolve();

    expect(PROPS.onPicked).not.toHaveBeenCalled();
    expect(PROPS.onError).not.toHaveBeenCalled();
  });
});

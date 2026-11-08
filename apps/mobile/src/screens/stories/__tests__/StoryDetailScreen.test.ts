import type { Story, StoryMedia } from '../../../api/stories';
import FeedVideo from '../../../components/FeedVideo';
import MediaImage from '../../../components/MediaImage';
import VideoThumbnail from '../../../components/VideoThumbnail';
import StoryDetailScreen from '../StoryDetailScreen';

/**
 * The Story Detail media strip draws the server's renditions (KNOT-036-fix4).
 *
 * The screen holds state and cannot be called without a hook stub, and this
 * project has no renderer (JEST is `testEnvironment: 'node'`, KNOT-020b). So
 * React's primitives are stubbed the way `StoryVersionScreen.test.ts` does it:
 * `useEffect` never runs (the scripted state below IS the loaded state),
 * `useCallback` returns the callback unchanged, and `useState` is fed a positional
 * sequence.
 *
 * The sequence is: story, version count, loading, error. The remaining four
 * (report visibility, active page, mute, carousel visibility) keep their initial
 * values. The pager's height is not state at all any more: it is derived from the
 * payload, which is what keeps the carousel from resizing (KNOT-048-fix2).
 *
 * `CarouselPage` is a child component, so it is never invoked here: the only
 * `Image` in the evaluated tree is the strip's, which makes the source assertion
 * below unambiguous.
 */
let mockStateValues: unknown[] = [];
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
    return [value === undefined ? initial : value, () => undefined];
  },
}));

jest.mock('react-native', () => ({
  ActivityIndicator: 'ActivityIndicator',
  Animated: { createAnimatedComponent: (component: unknown) => component },
  Easing: { bezier: () => () => 0 },
  Image: 'Image',
  Platform: { OS: 'android' },
  Pressable: 'Pressable',
  ScrollView: 'ScrollView',
  StatusBar: Object.assign(() => null, { currentHeight: 24 }),
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
  useWindowDimensions: () => ({ height: 844, width: 390 }),
}));

jest.mock('phosphor-react-native', () => ({
  ArrowLeft: () => null,
  Flag: () => null,
  GitBranch: () => null,
}));

// FeedVideo (the carousel's video page) imports the native player.
jest.mock('react-native-video', () => ({ __esModule: true, default: 'Video' }));

jest.mock('../../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));

const API_BASE = 'http://localhost:8080';

/** An image whose thumbnail the server generated. */
const imageMedia: StoryMedia = {
  id: 'media-1',
  media_type: 'image',
  source: 'gallery',
  mime_type: 'image/jpeg',
  width: 1200,
  height: 800,
  duration_ms: null,
  size_bytes: 1_000,
  display_order: 0,
  created_at: '2026-10-01T00:00:00Z',
  url: '/stories/story-1/media/media-1/content?variant=display',
  thumbnail_url: '/stories/story-1/media/media-1/content?variant=thumb',
  medium_url: '/stories/story-1/media/media-1/content?variant=medium',
  // The same display in a codec devices below Android 12 / iOS 16 can decode
  // (KNOT-038-fix3).
  display_webp_url: '/stories/story-1/media/media-1/content?variant=display&format=webp',
};

/** A video: its full rendition is the stream, and it has no cover. */
const videoMedia: StoryMedia = {
  id: 'media-2',
  media_type: 'video',
  source: 'camera',
  mime_type: 'video/mp4',
  width: 1080,
  height: 1920,
  duration_ms: 5_000,
  size_bytes: 2_000,
  display_order: 1,
  created_at: '2026-10-01T00:00:00Z',
  url: '/stories/story-1/media/media-2/content?variant=stream',
  thumbnail_url: null,
  medium_url: null,
};

/** The same image on a server that generated no thumbnail. */
const legacyImage: StoryMedia = {
  ...imageMedia,
  thumbnail_url: null,
  medium_url: null,
  // A row written before KNOT-038-fix3 has one display codec only.
  display_webp_url: null,
};

function storyWith(media: readonly StoryMedia[]): Story {
  return {
    id: 'story-1',
    author_id: 'user-root',
    author_display_name: 'Nomsa Khan',
    author_avatar_url: null,
    root_version_id: 'version-0',
    pillar: 'heritage',
    language: 'eng',
    title: 'The original',
    body: 'The original body.',
    approximate_location: 'Manguzi',
    media_urls: [],
    created_at: '2026-10-01T00:00:00Z',
    updated_at: '2026-10-01T00:00:00Z',
    media,
    version_count: 3,
  };
}

type ElementLike = { type?: unknown; props?: Record<string, unknown> };

/** Visits every element in a directly-called component's tree, depth-first. */
function collect(node: unknown, visit: (element: ElementLike) => void): void {
  if (Array.isArray(node)) {
    for (const child of node) {
      collect(child, visit);
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
  collect(element.props?.children, visit);
}

/** Every element of one component type. */
function elementsOfType(tree: unknown, type: unknown): ElementLike[] {
  const found: ElementLike[] = [];
  collect(tree, (element) => {
    if (element.type === type) {
      found.push(element);
    }
  });
  return found;
}

/** Every element whose component function has the given name (file-local children). */
function elementsByFunctionName(tree: unknown, name: string): ElementLike[] {
  const found: ElementLike[] = [];
  collect(tree, (element) => {
    if (typeof element.type === 'function' && element.type.name === name) {
      found.push(element);
    }
  });
  return found;
}

/** Calls the screen in its loaded state with the given media. */
function renderLoaded(media: readonly StoryMedia[]): unknown {
  mockStateValues = [storyWith(media), 3, false, undefined];
  mockStateCursor = 0;

  return StoryDetailScreen({
    id: 'story-1',
    token: 'token',
    onBack: () => undefined,
    onAdapt: () => undefined,
    onViewTree: () => undefined,
    onConversation: () => undefined,
    onOpenUserProfile: () => undefined,
    onOpenMedia: () => undefined,
  });
}

describe('StoryDetailScreen media strip', () => {
  it('draws the image tile from the thumbnail and the video tile from a placeholder', () => {
    const tree = renderLoaded([imageMedia, videoMedia]);

    const images = elementsOfType(tree, 'Image');
    expect(images).toHaveLength(1);
    expect(images[0].props?.source).toEqual({
      uri: API_BASE + '/stories/story-1/media/media-1/content?variant=thumb',
    });

    const video = elementsOfType(tree, VideoThumbnail);
    expect(video).toHaveLength(1);
    // The server generates no poster for a video any more, so the tile draws its
    // sunken placeholder and play disc (KNOT-037-fix10).
    expect(video[0].props?.posterUrl).toBeUndefined();
  });

  it('falls back to the display image when there is no thumbnail', () => {
    const tree = renderLoaded([legacyImage, videoMedia]);

    const images = elementsOfType(tree, 'Image');
    expect(images).toHaveLength(1);
    expect(images[0].props?.source).toEqual({
      uri: API_BASE + '/stories/story-1/media/media-1/content?variant=display',
    });

    const video = elementsOfType(tree, VideoThumbnail);
    expect(video).toHaveLength(1);
    expect(video[0].props?.posterUrl).toBeUndefined();
  });
});

describe('StoryDetailScreen carousel', () => {
  it('draws a photo from the WebP display and a video from its stream', () => {
    const tree = renderLoaded([imageMedia, videoMedia]);

    const pages = elementsByFunctionName(tree, 'CarouselPage');
    expect(pages).toHaveLength(2);

    // The pages are children, so they are invoked directly. Their hook state starts
    // empty: the shape is the payload's own dimensions.
    const draw = (index: number): unknown => {
      mockStateValues = [];
      mockStateCursor = 0;
      const page = pages[index];
      return (page.type as (props: unknown) => unknown)(page.props);
    };

    // AVIF is undecodable below Android 12 / iOS 16 and fails silently, so the page
    // asks for the WebP display and keeps the AVIF address only as the fallback
    // (KNOT-038-fix3). Two layers are handed to the one image renderer: the 400px
    // thumbnail the card already had, then the display over it (KNOT-045,
    // KNOT-051).
    const imagePage = elementsOfType(draw(0), MediaImage);
    expect(imagePage).toHaveLength(1);
    expect(imagePage[0].props?.stillUri).toBe(
      API_BASE + '/stories/story-1/media/media-1/content?variant=thumb',
    );
    expect(imagePage[0].props?.uri).toBe(
      API_BASE + '/stories/story-1/media/media-1/content?variant=display&format=webp',
    );

    const videoPage = elementsOfType(draw(1), FeedVideo);
    expect(videoPage).toHaveLength(1);
    expect(videoPage[0].props?.uri).toBe(API_BASE + videoMedia.url);
  });

  it('keeps the AVIF display when the server sent no WebP one', () => {
    const tree = renderLoaded([legacyImage, videoMedia]);

    const pages = elementsByFunctionName(tree, 'CarouselPage');
    expect(pages).toHaveLength(2);

    mockStateValues = [];
    mockStateCursor = 0;
    const page = pages[0];
    const imagePage = elementsOfType(
      (page.type as (props: unknown) => unknown)(page.props),
      MediaImage,
    );

    expect(imagePage).toHaveLength(1);
    expect(imagePage[0].props?.uri).toBe(API_BASE + legacyImage.url);
  });
});

describe('StoryDetailScreen media frame', () => {
  /** The pager's own style height, which is what the carousel actually occupies. */
  function pagerHeight(tree: unknown): number | undefined {
    const pager = elementsOfType(tree, 'ScrollView').find(
      (element) => element.props?.pagingEnabled === true,
    );
    const style = pager?.props?.style as { height?: number } | undefined;
    return style?.height;
  }

  it('reserves a positive height for a clip the payload cannot size, so there is no white gap', () => {
    // No dimensions on the wire: the old rule drew a square placeholder and then
    // collapsed once the player reported the clip's real shape, which is what left
    // the reader with a shrunken picture and the text pushed around (KNOT-048-fix2).
    const tree = renderLoaded([{ ...videoMedia, width: null, height: null }]);
    const height = pagerHeight(tree);

    expect(height).toBeDefined();
    expect(height).toBeGreaterThan(0);
    // The design's canonical 4:5 frame: 390 / 0.8 = 487.5.
    expect(height).toBeCloseTo(390 / (4 / 5), 6);
  });

  it('uses the payload ratio when the media reports its dimensions', () => {
    const tree = renderLoaded([imageMedia]);

    // 1200x800 is 3:2 landscape, so the frame is short rather than tall.
    expect(pagerHeight(tree)).toBeCloseTo(390 / (1200 / 800), 6);
  });

  it('caps a tall portrait at 4:5 instead of running off the page', () => {
    const tree = renderLoaded([videoMedia]);

    expect(videoMedia.height).toBeGreaterThan(videoMedia.width ?? 0);
    expect(pagerHeight(tree)).toBeCloseTo(390 / (4 / 5), 6);
  });

  it('gives every page the same height, so the pager is one box rather than a row of unequal cells', () => {
    const tree = renderLoaded([imageMedia, { ...videoMedia, width: null, height: null }]);
    const pages = elementsByFunctionName(tree, 'CarouselPage');

    expect(pages).toHaveLength(2);
    const heights = pages.map((page) => page.props?.height as number);

    expect(new Set(heights).size).toBe(1);
    expect(heights[0]).toBe(pagerHeight(tree));
    expect(heights[0]).toBeGreaterThan(0);
  });
});

import type { Story } from '../../../api/stories';
import type { StoryVersion } from '../../../api/versions';
import AuthorLine from '../../../components/AuthorLine';
import Button from '../../../components/Button';
import PillarTag from '../../../components/PillarTag';
import ReactionBar from '../../../components/ReactionBar';
import StoryVersionScreen from '../StoryVersionScreen';

/**
 * The Version Detail reads its version and then the parent story, so it holds
 * state and cannot be called without a hook stub. This project has no renderer
 * and must not add one, so React's primitives are stubbed: `useEffect` never
 * runs (the fixture state below is the loaded state), `useCallback` returns the
 * callback unchanged, and `useState` is fed a scripted sequence.
 *
 * The sequence is positional because React hooks are called in a fixed order:
 * version, parent story, loading, error, report visibility, active page, media
 * shapes, mute, and carousel visibility. Only the first three need a value; the
 * rest keep their initial values (KNOT-026).
 */
let mockStateValues: unknown[] = [];
let mockStateCursor = 0;

jest.mock('react', () => ({
  ...jest.requireActual('react'),
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
  DotsThree: () => null,
  GitBranch: () => null,
}));

// FeedVideo (the carousel's video page) imports the native player.
jest.mock('react-native-video', () => ({ __esModule: true, default: 'Video' }));

jest.mock('../../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));

/** A story's first media item: an image. */
const storyMedia = {
  id: 'media-1',
  media_type: 'image' as const,
  source: 'gallery' as const,
  mime_type: 'image/jpeg',
  width: 1200,
  height: 800,
  duration_ms: null,
  size_bytes: 1_000,
  display_order: 0,
  created_at: '2026-10-01T00:00:00Z',
  url: '/stories/story-1/media/media-1/content',
};

/** The story's second media item: a video, so both carousel branches exist. */
const storyVideo = {
  id: 'media-2',
  media_type: 'video' as const,
  source: 'camera' as const,
  mime_type: 'video/mp4',
  width: 1080,
  height: 1920,
  duration_ms: 5_000,
  size_bytes: 2_000,
  display_order: 1,
  created_at: '2026-10-01T00:00:00Z',
  url: '/stories/story-1/media/media-2/content',
};

const attributionLine = "Adapted from Ada Lovelace's original story in English into Zulu.";

/** The adaptation being read: the ADAPTER's text, with its server sentence. */
const version: StoryVersion = {
  id: 'version-1',
  story_id: 'story-1',
  parent_version_id: 'version-0',
  author_id: 'user-adapter',
  author_display_name: 'Bongani Ncube',
  author_avatar_url: null,
  language: 'zul',
  title: 'In the language of the tide',
  body: 'The body of the adaptation.',
  adaptation_note: 'A note from the adapter.',
  created_at: '2026-10-02T00:00:00Z',
  updated_at: '2026-10-02T00:00:00Z',
  author_rooted: null,
  my_reactions: [],
  attribution_line: attributionLine,
  version_depth: 1,
};

/**
 * The parent story. Its media, pillar, place, and version count are what the
 * version screen inherits — a version carries none of them itself.
 */
const story: Story = {
  id: 'story-1',
  author_id: 'user-root',
  root_version_id: 'version-0',
  pillar: 'heritage',
  language: 'eng',
  title: 'The original',
  body: 'The original body.',
  approximate_location: 'Manguzi',
  media_urls: [],
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
  media: [storyMedia, storyVideo],
  version_count: 3,
};

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

/** Flattens the text a node would draw. */
function textContent(node: unknown): string {
  if (typeof node === 'string' || typeof node === 'number') {
    return String(node);
  }
  if (Array.isArray(node)) {
    return node.map(textContent).join('');
  }
  if (node !== null && typeof node === 'object') {
    return textContent((node as ElementLike).props?.children);
  }
  return '';
}

/** Every string drawn by a `Text` element in the tree. */
function drawnText(tree: unknown): string[] {
  const found: string[] = [];
  collect(tree, (element) => {
    if (element.type === 'Text') {
      found.push(textContent(element.props?.children));
    }
  });
  return found;
}

/**
 * The ids of the media the carousel pages were built with. A page is a child
 * component, so this reads its props rather than its drawn output — which is
 * exactly the point: a version renders the PARENT STORY's media.
 */
function carouselMediaIds(tree: unknown): string[] {
  const found: string[] = [];
  collect(tree, (element) => {
    const media = element.props?.media as { id?: unknown } | undefined;
    if (media !== undefined && typeof media.id === 'string') {
      found.push(media.id);
    }
  });
  return found;
}

/** Finds the first element of a given component type. */
function findByType(tree: unknown, type: unknown): ElementLike | undefined {
  let found: ElementLike | undefined;
  collect(tree, (element) => {
    if (found === undefined && element.type === type) {
      found = element;
    }
  });
  return found;
}

/** Calls the screen in its loaded state: a version and its parent story. */
function renderLoaded(): unknown {
  mockStateValues = [version, story, false];
  mockStateCursor = 0;

  return StoryVersionScreen({
    versionId: version.id,
    token: 'token',
    onBack: () => undefined,
    onAdapt: () => undefined,
    onOpenStory: () => undefined,
    onViewTree: () => undefined,
    onOpenUserProfile: () => undefined,
  });
}

describe('StoryVersionScreen', () => {
  it("renders the attribution line and the parent story's media", () => {
    const tree = renderLoaded();

    expect(drawnText(tree)).toContain(attributionLine);
    expect(carouselMediaIds(tree)).toEqual([storyMedia.id, storyVideo.id]);
  });

  it('draws the adapter in the Story Detail author block and the two version CTAs', () => {
    const tree = renderLoaded();

    const author = findByType(tree, AuthorLine);
    expect(author?.props).toMatchObject({
      displayName: version.author_display_name,
      size: 'large',
      layout: 'stacked',
      timeFormat: 'short',
    });

    const labels: unknown[] = [];
    collect(tree, (element) => {
      if (element.type === Button) {
        labels.push(element.props?.label);
      }
    });
    expect(labels).toEqual(['View original story', 'Adapt this version']);
  });

  it("shows the inherited pillar and the language tree's version count", () => {
    const tree = renderLoaded();

    expect(findByType(tree, PillarTag)?.props).toMatchObject({
      pillar: story.pillar,
      place: story.approximate_location,
      variant: 'plain',
    });

    const treeButton = findByType(tree, 'Pressable')?.props?.accessibilityLabel;
    expect(treeButton).toBe('View the language tree, 3 versions');

    expect(findByType(tree, ReactionBar)?.props).toMatchObject({
      layout: 'grid',
      entityType: 'version',
      entityId: version.id,
    });
  });
});

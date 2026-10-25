import type { ImageLoadEventData, NativeSyntheticEvent } from 'react-native';

import { MAX_PORTRAIT_ASPECT_RATIO } from '../../utils/mediaFrame';
import { spacing } from '../../theme';
import { ShareNetwork } from 'phosphor-react-native';

import AuthorLine from '../AuthorLine';
import FeedEntry from '../FeedEntry';
import FeedVideo from '../FeedVideo';
import PillarTag from '../PillarTag';
import SocialProofHeader from '../SocialProofHeader';

/**
 * FeedEntry resolves its media frame from the payload or from the media's own
 * on-load size, so it holds state and can no longer be covered by the hookless
 * direct-call suite it used to live in. This project has no renderer and must not
 * add one, so React's hook primitives are stubbed instead: `useState` returns its
 * initial value (the pre-load state, which is what a static call can honestly
 * assert) and `useCallback` returns the callback unchanged. The component is then
 * called as a plain function exactly as before.
 *
 * The decision the hook makes — payload, then loaded size, then unknown — is
 * exhaustively covered as a pure rule in `utils/__tests__/mediaFrame.test.ts`,
 * which is the part that can be tested without a renderer (KNOT-020g-fix7).
 */
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useContext: () => jest.requireActual('../../theme/ThemeContext').lightTheme,
  useCallback: (callback: unknown) => callback,
  useState: (initial: unknown) => [initial, () => undefined],
}));

jest.mock('react-native', () => ({
  Image: 'Image',
  Pressable: 'Pressable',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

jest.mock('phosphor-react-native', () => ({
  DotsThree: () => null,
  Images: () => null,
  ShareNetwork: () => null,
  X: () => null,
}));

// FeedEntry renders a video preview with react-native-video, which is a native
// module; stub it to a host component so the direct-call test can build the
// element (KNOT-020c-fix2).
jest.mock('react-native-video', () => ({ __esModule: true, default: 'Video' }));

jest.mock('../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));

/**
 * Collects every element type in a directly-called component's element tree,
 * depth-first. There is no renderer, so a structural assertion is the only way to
 * prove which child component a parent chose to build (KNOT-020c-fix3).
 */
function elementTypes(node: unknown, found: unknown[] = []): unknown[] {
  if (Array.isArray(node)) {
    for (const child of node) {
      elementTypes(child, found);
    }
    return found;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return found;
  }

  const element = node as { type?: unknown; props?: { children?: unknown } };
  if (element.type !== undefined) {
    found.push(element.type);
  }
  if (element.props !== undefined) {
    elementTypes(element.props.children, found);
  }

  return found;
}

/**
 * Finds the first element of the given component type in a directly-called
 * component's element tree, so a parent's choice of child props can be asserted
 * without a renderer (KNOT-020c-fix4).
 */
function findElementByType(
  node: unknown,
  type: unknown,
): { props?: Record<string, unknown> } | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findElementByType(child, type);
      if (found !== undefined) {
        return found;
      }
    }
    return undefined;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return undefined;
  }

  const element = node as { type?: unknown; props?: { children?: unknown } };
  if (element.type === type) {
    return element as { props?: Record<string, unknown> };
  }
  if (element.props !== undefined) {
    return findElementByType(element.props.children, type);
  }
  return undefined;
}

/**
 * The first element whose accessibility label matches, depth-first, so a nested
 * tap target can be driven without a renderer.
 */
function findElementByLabel(
  node: unknown,
  label: string,
): { props?: Record<string, unknown> } | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findElementByLabel(child, label);
      if (found !== undefined) {
        return found;
      }
    }
    return undefined;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return undefined;
  }

  const element = node as {
    props?: { children?: unknown; accessibilityLabel?: unknown };
  };
  if (element.props?.accessibilityLabel === label) {
    return element as { props?: Record<string, unknown> };
  }
  if (element.props !== undefined) {
    return findElementByLabel(element.props.children, label);
  }
  return undefined;
}

/** The entry every assertion below is built from. */
const story = {
  id: 'story-1',
  title: 'How my grandmother counted the days',
  pillar: 'heritage' as const,
  preview: 'She kept a string of knots.',
  language: 'zap',
  place: 'Oaxaca, Mexico',
  author_display_name: 'Nomsa Khan',
  author_avatar_url: null,
  author_rooted: { place: 'Oaxaca', duration_bucket: 'many_years' },
  created_at: '2026-10-09T12:00:00.000Z',
  version_count: 4,
  comment_count: 14,
  reactions: {
    rings_true: 128,
    know_it_differently: 19,
    adds_something_new: 7,
    needs_a_source: 2,
  },
};

describe('FeedEntry', () => {
  it('renders a full entry without invoking the press handlers', () => {
    const onPress = jest.fn();
    const onOpenAuthor = jest.fn();

    const element = FeedEntry({ story, onPress, onOpenAuthor });

    expect(element).toBeTruthy();
    expect(onPress).not.toHaveBeenCalled();
    expect(onOpenAuthor).not.toHaveBeenCalled();
  });

  it('renders an image preview and a video preview with a count badge', () => {
    expect(
      FeedEntry({
        story: {
          ...story,
          first_media_type: 'image',
          first_media_url: '/stories/x/media/y/content',
        },
        onPress: () => undefined,
      }),
    ).toBeTruthy();
    expect(
      FeedEntry({
        story: {
          ...story,
          first_media_type: 'video',
          first_media_url: '/stories/x/media/y/content',
          media_count: 5,
        },
        isMuted: true,
        isVisible: true,
        onToggleMute: () => undefined,
        onPress: () => undefined,
      }),
    ).toBeTruthy();
  });

  it('hands the viewer a complete story, with the tapped media, on the first tap', () => {
    const onOpenMedia = jest.fn();
    const element = FeedEntry({
      story: {
        ...story,
        first_media_type: 'image',
        first_media_url: '/stories/x/media/y/content',
      },
      onPress: () => undefined,
      onOpenMedia,
    });
    const mediaTarget = findElementByLabel(element, `Open media from ${story.title}`);

    expect(mediaTarget).toBeDefined();
    (mediaTarget?.props?.onPress as () => void)();

    expect(onOpenMedia).toHaveBeenCalledTimes(1);
    const [items, index, viewerStory] = onOpenMedia.mock.calls[0] as [
      readonly { readonly url: string }[],
      number,
      { readonly storyId: string; readonly media: readonly { readonly url: string }[] },
    ];
    expect(items).toHaveLength(1);
    expect(index).toBe(0);
    // The viewer is handed a complete story — id and media — so it opens in the
    // same fixed layout Story Detail opens, without waiting for a fetch
    // (KNOT-020k-fix11).
    expect(viewerStory.storyId).toBe('story-1');
    expect(viewerStory.media).toHaveLength(1);
    expect(viewerStory.media[0]?.url).toBe('/stories/x/media/y/content');
  });

  it('renders FeedVideo for a video and never for an image', () => {
    const image = FeedEntry({
      story: {
        ...story,
        first_media_type: 'image',
        first_media_url: '/stories/x/media/y/content',
      },
      onPress: () => undefined,
    });
    expect(elementTypes(image)).toContain('Image');
    expect(elementTypes(image)).not.toContain(FeedVideo);

    const video = FeedEntry({
      story: {
        ...story,
        first_media_type: 'video',
        first_media_url: '/stories/x/media/y/content',
        media_count: 5,
      },
      isMuted: false,
      isVisible: true,
      onToggleMute: () => undefined,
      onPress: () => undefined,
    });
    expect(elementTypes(video)).toContain(FeedVideo);
    expect(elementTypes(video)).not.toContain('Image');
  });

  it('renders with no author fields, no media, and every count zero', () => {
    expect(
      FeedEntry({
        story: {
          ...story,
          author_display_name: null,
          author_avatar_url: null,
          author_rooted: null,
          reactions: undefined,
          comment_count: 0,
          language_count: 0,
          media_count: 0,
          version_count: 0,
        },
        onPress: () => undefined,
      }),
    ).toBeTruthy();
  });

  // The feed's Rooted line (KNOT-020c-fix4): the story's author_rooted is handed
  // to AuthorLine, which draws the quiet second line.
  it('passes the author Rooted summary to AuthorLine for the quiet second line', () => {
    const element = FeedEntry({ story, onPress: () => undefined });
    const authorLine = findElementByType(element, AuthorLine);

    expect(authorLine).toBeDefined();
    expect(authorLine?.props?.rooted).toEqual({ place: 'Oaxaca', duration_bucket: 'many_years' });
    // The feed's social-card block: name, Rooted, time, with a left avatar
    // (KNOT-023b).
    expect(authorLine?.props?.layout).toBe('stacked');
    expect(authorLine?.props?.size).toBe('large');
    // The feed is the only caller of the compact time format.
    expect(authorLine?.props?.timeFormat).toBe('short');
  });

  it('passes a null Rooted summary when the author has no signal', () => {
    const element = FeedEntry({
      story: { ...story, author_rooted: null },
      onPress: () => undefined,
    });
    const authorLine = findElementByType(element, AuthorLine);

    expect(authorLine?.props?.rooted).toBeNull();
  });

  /**
   * The frame's shape. Every style object in the tree is walked and the one
   * carrying an `aspectRatio` is read, which is how the resolved ratio is asserted
   * without a renderer (KNOT-020g-fix7).
   */
  function collectStyleRatios(node: unknown, found: number[] = []): number[] {
    if (Array.isArray(node)) {
      for (const child of node) {
        collectStyleRatios(child, found);
      }
      return found;
    }
    if (node === null || node === undefined || typeof node !== 'object') {
      return found;
    }

    const element = node as {
      props?: { style?: unknown; children?: unknown };
    };
    const style = element.props?.style;
    for (const entry of Array.isArray(style) ? style : [style]) {
      const candidate = entry as { aspectRatio?: number } | undefined;
      if (candidate?.aspectRatio !== undefined) {
        found.push(candidate.aspectRatio);
      }
    }
    if (element.props !== undefined) {
      collectStyleRatios(element.props.children, found);
    }
    return found;
  }

  /** The single ratio the entry's media frame carries. */
  function frameAspectRatio(element: unknown): number | undefined {
    const ratios = collectStyleRatios(element);
    expect(ratios).toHaveLength(1);
    return ratios[0];
  }

  const withMedia = {
    ...story,
    first_media_type: 'image' as const,
    first_media_url: '/stories/x/media/y/content',
  };

  it('frames media at the payload ratio up to the 4:5 cap, so no frame is taller than that', () => {
    const portrait = FeedEntry({
      story: { ...withMedia, first_media_width: 1080, first_media_height: 1920 },
      onPress: () => undefined,
    });

    // 9:16 is taller than a list frame may be, so it is capped at 4:5 — the frame
    // is at most `width x 1.25` (KNOT-020g-fix12).
    expect(frameAspectRatio(portrait)).toBe(MAX_PORTRAIT_ASPECT_RATIO);
  });

  it('leaves a 4:5 photo exactly as it is, so the cap only bites where it must', () => {
    const element = FeedEntry({
      story: { ...withMedia, first_media_width: 1000, first_media_height: 1250 },
      onPress: () => undefined,
    });

    expect(frameAspectRatio(element)).toBeCloseTo(0.8, 6);
  });

  it('frames media at the payload ratio when it is landscape', () => {
    const element = FeedEntry({
      story: { ...withMedia, first_media_width: 1920, first_media_height: 1080 },
      onPress: () => undefined,
    });

    expect(frameAspectRatio(element)).toBeCloseTo(1920 / 1080, 6);
  });

  it('falls back to the square placeholder, never a fixed landscape frame', () => {
    const element = FeedEntry({ story: withMedia, onPress: () => undefined });

    expect(frameAspectRatio(element)).toBe(1);
  });

  it('asks the image for its natural size, so a payload with no dimensions can settle', () => {
    const element = FeedEntry({ story: withMedia, onPress: () => undefined });
    const image = findElementByType(element, 'Image');
    const onLoad = image?.props?.onLoad as
      ((event: NativeSyntheticEvent<ImageLoadEventData>) => void) | undefined;

    expect(typeof onLoad).toBe('function');

    // It reads the decoded media's own size, which is what the frame re-shapes to.
    const event = { nativeEvent: { source: { width: 300, height: 700, uri: 'u' } } };
    expect(() => onLoad?.(event as NativeSyntheticEvent<ImageLoadEventData>)).not.toThrow();
  });

  it('hands the video the same size pass-through as the image', () => {
    const element = FeedEntry({
      story: { ...withMedia, first_media_type: 'video' },
      onPress: () => undefined,
    });
    const video = findElementByType(element, FeedVideo);

    expect(video).toBeDefined();
    expect(typeof video?.props?.onLoadSize).toBe('function');
  });

  describe('the version variant', () => {
    const versionFeedItem = {
      ...withMedia,
      parent_author_id: 'author-parent',
      parent_author_display_name: 'MaEnzy',
      parent_author_avatar_url: '/users/author-parent/avatar?v=p.png',
      root_author_id: 'author-root',
      root_author_display_name: 'Ada Lovelace',
      root_author_avatar_url: '/users/author-root/avatar?v=a.png',
    };

    it('hands the hereditary credit to the social-proof header', () => {
      const sentence = "Adapted by Nomsa Khan from Ada Lovelace's original story in English.";
      const element = FeedEntry({
        story: versionFeedItem,
        variant: 'version',
        versionNumber: 2,
        attributionLine: sentence,
        onPress: () => undefined,
      });

      const header = findElementByType(element, SocialProofHeader);
      expect(header?.props).toMatchObject({
        attributionLine: sentence,
        originalAuthorName: 'Ada Lovelace',
      });

      const lineage = header?.props?.lineageAvatars as ReadonlyArray<{
        name: string;
        uri: string | undefined;
      }>;
      expect(lineage.map((actor) => actor.name)).toEqual(['MaEnzy', 'Ada Lovelace']);
      expect(lineage.map((actor) => actor.uri)).toEqual([
        'http://localhost:8080/users/author-parent/avatar?v=p.png',
        'http://localhost:8080/users/author-root/avatar?v=a.png',
      ]);
    });
  });

  describe('the social-proof header', () => {
    // The founder's shape: the poster (Nomsa) is version 3's author, MaEnzy wrote
    // the version it adapts, and Ada wrote the original. The header must receive
    // the lineage -- MaEnzy and Ada -- never the poster, who is the AuthorLine
    // below (KNOT-022-fix2, KNOT-028).
    const sentence =
      "Adapted by Nomsa Khan from MaEnzy's Zulu version of Ada Lovelace's original story in English.";
    const adapted = {
      ...story,
      author_avatar_url: '/users/author-poster/avatar?v=poster.png',
      parent_author_id: 'author-parent',
      parent_author_display_name: 'MaEnzy',
      parent_author_avatar_url: '/users/author-parent/avatar?v=p.png',
      root_author_id: 'author-root',
      root_author_display_name: 'Ada Lovelace',
      root_author_avatar_url: '/users/author-root/avatar?v=a.png',
    };

    it('hands the lineage and the sentence to the header', () => {
      const element = FeedEntry({
        story: adapted,
        variant: 'version',
        versionNumber: 3,
        attributionLine: sentence,
        onPress: () => undefined,
      });

      const header = findElementByType(element, SocialProofHeader);
      expect(header?.props).toMatchObject({
        attributionLine: sentence,
        originalAuthorName: 'Ada Lovelace',
      });

      const lineage = header?.props?.lineageAvatars as ReadonlyArray<{
        name: string;
        uri: string | undefined;
      }>;
      expect(lineage.map((actor) => actor.name)).toEqual(['MaEnzy', 'Ada Lovelace']);
      expect(lineage.map((actor) => actor.uri)).toEqual([
        'http://localhost:8080/users/author-parent/avatar?v=p.png',
        'http://localhost:8080/users/author-root/avatar?v=a.png',
      ]);

      // The poster is deliberately not in the stack; they are the author line.
      expect(lineage.map((actor) => actor.uri)).not.toContain(
        'http://localhost:8080/users/author-poster/avatar?v=poster.png',
      );
    });

    it('draws one lineage face when the parent is the original author', () => {
      const element = FeedEntry({
        story: {
          ...adapted,
          parent_author_id: 'author-root',
          parent_author_display_name: 'Ada Lovelace',
          parent_author_avatar_url: '/users/author-root/avatar?v=a.png',
        },
        variant: 'version',
        versionNumber: 2,
        attributionLine: "Adapted by Nomsa Khan from Ada Lovelace's original story in English.",
        onPress: () => undefined,
      });

      const lineage = findElementByType(element, SocialProofHeader)?.props?.lineageAvatars as
        ReadonlyArray<{ name: string; uri: string | undefined }> | undefined;

      expect(lineage).toHaveLength(1);
      expect(lineage?.[0].name).toBe('Ada Lovelace');
      expect(lineage?.[0].uri).toBe('http://localhost:8080/users/author-root/avatar?v=a.png');
    });

    it('passes an original-story sentence through unchanged', () => {
      const element = FeedEntry({
        story: adapted,
        variant: 'version',
        versionNumber: 1,
        attributionLine: 'Original story by Ada Lovelace in English.',
        onPress: () => undefined,
      });

      expect(findElementByType(element, SocialProofHeader)?.props?.attributionLine).toBe(
        'Original story by Ada Lovelace in English.',
      );
    });

    it('draws no header for a story with no interactions', () => {
      const element = FeedEntry({
        story: { ...story, root_author_display_name: 'Ada Lovelace' },
        attributionLine: sentence,
        onPress: () => undefined,
      });

      // The header is universal now, but a story carries no hereditary credit,
      // so with no interactions it is handed an empty state and draws nothing
      // (KNOT-029).
      const header = findElementByType(element, SocialProofHeader);
      expect(header).toBeDefined();
      expect(header?.props).toMatchObject({
        attributionLine: '',
        originalAuthorName: '',
      });
      expect(header?.props?.lineageAvatars).toEqual([]);
      expect(header?.props?.interactions).toBeUndefined();
    });

    it('heads a story card with its interactions', () => {
      const interactions = [
        {
          type: 'reaction' as const,
          actor_display_name: 'Tycoon',
          actor_avatar_url: null,
          detail: 'rings_true',
        },
      ];
      const element = FeedEntry({
        story: { ...story, recent_interactions: interactions },
        onPress: () => undefined,
      });

      expect(findElementByType(element, SocialProofHeader)?.props?.interactions).toEqual(
        interactions,
      );
    });

    it('always marks a version card as adapted', () => {
      const element = FeedEntry({
        story: adapted,
        variant: 'version',
        versionNumber: 3,
        attributionLine: sentence,
        onPress: () => undefined,
      });

      const badge = findElementByType(element, AuthorLine)?.props?.badge;
      expect(badge).toBeTruthy();
      expect(findElementByType(badge, ShareNetwork)).toBeDefined();

      const badgeElement = badge as { props?: Record<string, unknown> };
      expect(badgeElement.props?.testID).toBe('adapt-badge');
      expect(badgeElement.props?.accessibilityLabel).toBe('Adapted story');
    });

    it('keeps the adaptation marker when interactions replace the attribution', () => {
      const element = FeedEntry({
        story: {
          ...adapted,
          recent_interactions: [
            {
              type: 'comment' as const,
              actor_display_name: 'Yantuntu',
              actor_avatar_url: null,
              detail: null,
            },
          ],
        },
        variant: 'version',
        versionNumber: 3,
        attributionLine: sentence,
        onPress: () => undefined,
      });

      expect(findElementByType(element, SocialProofHeader)?.props?.interactions).toBeDefined();
      expect(findElementByType(element, AuthorLine)?.props?.badge).toBeTruthy();
    });

    it('does not mark a story card as adapted', () => {
      const element = FeedEntry({ story, onPress: () => undefined });

      expect(findElementByType(element, AuthorLine)?.props?.badge).toBeNull();
    });

    it('passes nothing to credit when neither the sentence nor the root author exists', () => {
      const element = FeedEntry({
        story: { ...story, root_author_display_name: null },
        variant: 'version',
        versionNumber: 2,
        attributionLine: '',
        onPress: () => undefined,
      });

      // The header is handed the empty state and decides to draw nothing; no
      // "Version N" fallback is left behind (KNOT-025-fix4, KNOT-028).
      const header = findElementByType(element, SocialProofHeader);
      expect(header?.props?.attributionLine).toBe('');
      expect(header?.props?.originalAuthorName).toBe('');
      expect(collectText(element).some((value) => value.startsWith('Version '))).toBe(false);
    });

    it('hands the structured lineage to the fallback when the sentence is missing', () => {
      const element = FeedEntry({
        story: adapted,
        variant: 'version',
        versionNumber: 2,
        attributionLine: '',
        onPress: () => undefined,
      });

      expect(findElementByType(element, SocialProofHeader)?.props).toMatchObject({
        attributionLine: '',
        originalAuthorName: 'Ada Lovelace',
      });
      expect(collectText(element).some((value) => value.startsWith('Version '))).toBe(false);
    });
  });
});

/**
 * Finds the first element whose accessibility label satisfies the predicate, so a
 * link can be asserted by what a screen reader would announce rather than by its
 * position in the tree (KNOT-020g-fix12).
 */
function findByLabel(
  node: unknown,
  matches: (label: string) => boolean,
): { props?: Record<string, unknown> } | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findByLabel(child, matches);
      if (found !== undefined) {
        return found;
      }
    }
    return undefined;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return undefined;
  }

  const element = node as { props?: { accessibilityLabel?: unknown; children?: unknown } };
  const label = element.props?.accessibilityLabel;
  if (typeof label === 'string' && matches(label)) {
    return element as { props?: Record<string, unknown> };
  }
  if (element.props !== undefined) {
    return findByLabel(element.props.children, matches);
  }
  return undefined;
}

/** The "Read more" link, or undefined when the entry did not draw one. */
function readMoreLink(element: unknown): { props?: Record<string, unknown> } | undefined {
  return findByLabel(element, (label) => label.startsWith('Read more'));
}

describe('FeedEntry read more', () => {
  it('offers a link when the server marks the preview as truncated', () => {
    const onPress = jest.fn();
    const element = FeedEntry({ story: { ...story, preview_truncated: true }, onPress });

    const link = readMoreLink(element);
    expect(link).toBeDefined();

    // It is the entry's own handler, so the link opens the same thing the card
    // does — and the link's label names the story.
    expect(link?.props?.accessibilityLabel).toBe(
      'Read more of How my grandmother counted the days',
    );
    (link?.props?.onPress as () => void)();
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('offers no link for a body the server did not truncate', () => {
    const element = FeedEntry({ story, onPress: () => undefined });

    expect(readMoreLink(element)).toBeUndefined();
  });

  it('falls back to the text, so a server without the flag still gets a link', () => {
    const element = FeedEntry({
      story: { ...story, preview: 'A cut excerpt…' },
      onPress: () => undefined,
    });

    expect(readMoreLink(element)).toBeDefined();
  });

  it('lets the flag win over the text, so an ellipsis in a whole body is not a link', () => {
    const element = FeedEntry({
      story: { ...story, preview: 'He paused… then went on.', preview_truncated: false },
      onPress: () => undefined,
    });

    expect(readMoreLink(element)).toBeUndefined();
  });
});

/**
 * Collects every string child in a directly-called component's element tree, so
 * the text an entry draws can be asserted without a renderer (KNOT-022).
 */
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

/** The fixture the version-variant assertions below are built from. */
const version = {
  ...story,
  id: 'version-9',
  title: 'Imvula yokuqala',
  preview: 'Ugogo wathi imvula yokuqala ikhumbula wonke amagama.',
  language: 'zul',
};

const ATTRIBUTION = "Adapted by Nomsa Khan from Ada Lovelace's original story in English.";

describe('FeedEntry version variant', () => {
  it('labels an adaptation with its inherited pillar and no version tail', () => {
    const element = FeedEntry({
      story: version,
      variant: 'version',
      versionNumber: 2,
      attributionLine: ATTRIBUTION,
      onPress: () => undefined,
    });

    // The pillar here is the story's, inherited, and the tag is exactly the one a
    // story card draws: plain, with its place (KNOT-023, KNOT-022-fix1).
    const pillar = elementTypes(element);
    expect(pillar).toContain(PillarTag);
    expect(findElementByType(element, PillarTag)?.props?.pillar).toBe('heritage');
    expect(findElementByType(element, PillarTag)?.props?.variant).toBe('plain');
    expect(findElementByType(element, PillarTag)?.props?.place).toBe('Oaxaca, Mexico');

    // The credit ends after the language: no "Version N" anywhere on the card
    // (KNOT-025-fix4).
    const text = collectText(element);
    expect(text.some((value) => value.startsWith('Version '))).toBe(false);
    expect(findElementByType(element, SocialProofHeader)?.props?.attributionLine).toBe(ATTRIBUTION);
  });

  it('passes the attribution sentence to the header unchanged', () => {
    const element = FeedEntry({
      story: version,
      variant: 'version',
      versionNumber: 3,
      attributionLine: ATTRIBUTION,
      onPress: () => undefined,
    });

    expect(findElementByType(element, SocialProofHeader)?.props?.attributionLine).toBe(ATTRIBUTION);
  });

  it('omits the attribution line when the server could not resolve one', () => {
    const element = FeedEntry({
      story: version,
      variant: 'version',
      versionNumber: 2,
      attributionLine: '',
      onPress: () => undefined,
    });

    const text = collectText(element);
    expect(text).not.toContain(ATTRIBUTION);
    expect(text.some((value) => value.startsWith('Version '))).toBe(false);
    expect(findElementByType(element, SocialProofHeader)?.props?.attributionLine).toBe('');
  });

  it('ends the adaptation credit after the language, with no version tail', () => {
    const element = FeedEntry({
      story: version,
      variant: 'version',
      versionNumber: 3,
      attributionLine: ATTRIBUTION,
      onPress: () => undefined,
    });

    const text = collectText(element);
    expect(text.some((value) => value.startsWith('Version '))).toBe(false);
    expect(findElementByType(element, SocialProofHeader)?.props?.attributionLine).toBe(ATTRIBUTION);
  });

  it('still renders the story shape when the variant is not given', () => {
    const element = FeedEntry({ story, onPress: () => undefined });

    expect(elementTypes(element)).toContain(PillarTag);
    expect(collectText(element).some((value) => value.startsWith('Version '))).toBe(false);
    expect(collectText(element)).toContain(story.preview);
  });

  it('announces what a tap opens: the version for an adaptation, the story for a story', () => {
    const adaptation = FeedEntry({
      story: version,
      variant: 'version',
      versionNumber: 2,
      attributionLine: ATTRIBUTION,
      onPress: () => undefined,
    });
    expect(findByLabel(adaptation, (label) => label.startsWith('Open this version'))).toBeDefined();
    expect(
      findByLabel(adaptation, (label) => label === 'Open this version: Imvula yokuqala'),
    ).toBeDefined();

    const original = FeedEntry({ story, onPress: () => undefined });
    expect(
      findByLabel(
        original,
        (label) => label === 'Open the story: How my grandmother counted the days',
      ),
    ).toBeDefined();
  });

  it('keeps the rest of the card, so an adaptation reads like any other entry', () => {
    const element = FeedEntry({
      story: { ...version, preview_truncated: true },
      variant: 'version',
      versionNumber: 2,
      attributionLine: ATTRIBUTION,
      onPress: () => undefined,
      onOpenAuthor: () => undefined,
    });

    // The author line, the title, the preview, and the meta line are all still
    // drawn: only the framing row and the attribution differ.
    expect(findElementByType(element, AuthorLine)).toBeDefined();
    const text = collectText(element);
    expect(text).toContain(version.title);
    expect(text).toContain(version.preview);
    expect(findByLabel(element, (label) => label.startsWith('Read more'))).toBeDefined();
  });
});

/** The card's overflow control, or undefined when the card drew none. */
function postOptions(element: unknown): { props?: Record<string, unknown> } | undefined {
  return findByLabel(element, (label) => label === 'Post options');
}

/** The card's one-tap hide control, or undefined when the card drew none. */
function hideControl(element: unknown): { props?: Record<string, unknown> } | undefined {
  return findByLabel(element, (label) => label === 'Hide this post');
}

describe('FeedEntry post options', () => {
  it('draws no overflow control when the screen gave it no handler', () => {
    // Place Stories renders the same card without a menu, so the control must be
    // absent rather than dead (KNOT-031).
    const element = FeedEntry({ story, onPress: () => undefined, onOpenAuthor: () => undefined });

    expect(postOptions(element)).toBeUndefined();
  });

  it('draws a labelled control whose press opens the menu, never the card', () => {
    const onPress = jest.fn();
    const onOpenAuthor = jest.fn();
    const onOpenOptions = jest.fn();

    const element = FeedEntry({ story, onPress, onOpenAuthor, onOpenOptions });
    const control = postOptions(element);

    expect(control).toBeDefined();
    expect(control?.props?.accessibilityRole).toBe('button');
    expect(control?.props?.testID).toBe('post-options');

    (control?.props?.onPress as () => void)();

    // The control is nested inside the card's own Pressable. There is no renderer
    // here, so the isolation is asserted structurally: the control carries its own
    // handler, and no handler of the card's fired (KNOT-031).
    expect(onOpenOptions).toHaveBeenCalledTimes(1);
    expect(onPress).not.toHaveBeenCalled();
    expect(onOpenAuthor).not.toHaveBeenCalled();
  });

  it('gives the control the 44pt touch target the design system requires', () => {
    const element = FeedEntry({
      story,
      onPress: () => undefined,
      onOpenOptions: () => undefined,
    });
    const style = postOptions(element)?.props?.style as
      { height?: number; width?: number } | undefined;

    expect(style?.height).toBe(44);
    expect(style?.width).toBe(44);
  });

  it('keeps the author block beside the control, so neither is pushed away', () => {
    const withMenu = FeedEntry({
      story,
      onPress: () => undefined,
      onOpenAuthor: () => undefined,
      onOpenOptions: () => undefined,
    });

    // The AuthorLine and the control are siblings: the card still draws the author
    // block whether or not a menu is offered.
    expect(findElementByType(withMenu, AuthorLine)).toBeDefined();
    expect(postOptions(withMenu)).toBeDefined();
  });
});

describe('FeedEntry hide control', () => {
  it('draws no hide control when the screen gave it no handler', () => {
    // Place Stories reuses the card, so an unhandled quick hide must be absent
    // rather than a dead target (KNOT-032).
    const element = FeedEntry({
      story,
      onPress: () => undefined,
      onOpenOptions: () => undefined,
    });

    expect(hideControl(element)).toBeUndefined();
  });

  it('draws both quick actions when the screen supplies both handlers', () => {
    const element = FeedEntry({
      story,
      onPress: () => undefined,
      onOpenOptions: () => undefined,
      onHide: () => undefined,
    });

    // The pair is what the card is for: the menu for anything else, the `✕` for
    // the one-tap hide (KNOT-032).
    expect(postOptions(element)).toBeDefined();
    expect(hideControl(element)).toBeDefined();
    expect(hideControl(element)?.props?.testID).toBe('post-hide');
    expect(postOptions(element)?.props?.testID).toBe('post-options');
  });

  it('hides without opening the card or the menu', () => {
    const onPress = jest.fn();
    const onOpenOptions = jest.fn();
    const onHide = jest.fn();

    const element = FeedEntry({ story, onPress, onOpenOptions, onHide });
    const control = hideControl(element);

    expect(control).toBeDefined();
    expect(control?.props?.accessibilityRole).toBe('button');

    (control?.props?.onPress as () => void)();

    // Nested targets: the `✕` carries its own handler, and neither the card nor
    // the menu was opened by the tap (KNOT-031, KNOT-032).
    expect(onHide).toHaveBeenCalledTimes(1);
    expect(onPress).not.toHaveBeenCalled();
    expect(onOpenOptions).not.toHaveBeenCalled();
  });

  it('gives the hide control the 44pt touch target the design system requires', () => {
    const element = FeedEntry({
      story,
      onPress: () => undefined,
      onHide: () => undefined,
    });
    const style = hideControl(element)?.props?.style as
      { height?: number; width?: number } | undefined;

    expect(style?.height).toBe(44);
    expect(style?.width).toBe(44);
  });

  it('draws neither quick action when the screen supplies no handlers', () => {
    // Place Stories renders this card with nothing to hide and no post menu, so it
    // must gain no dead targets (KNOT-032, KNOT-032-fix1).
    const element = FeedEntry({ story, onPress: () => undefined, onOpenAuthor: () => undefined });

    expect(postOptions(element)).toBeUndefined();
    expect(hideControl(element)).toBeUndefined();
  });

  it('opens a social-proof card at the compact band inset, and other cards at rest', () => {
    // A card with a header opens with `spacing.sm`: the band supplies the rest of
    // the top breathing room itself (KNOT-032-fix2).
    const withHeader = FeedEntry({
      story: {
        ...story,
        recent_interactions: [
          {
            type: 'reaction',
            actor_display_name: 'Tycoon',
            actor_avatar_url: null,
            detail: 'rings_true',
          },
        ],
      },
      onPress: () => undefined,
      showBottomHairline: false,
    });
    const withHeaderStyle = withHeader.props.style as readonly Record<string, unknown>[];
    expect(withHeaderStyle[0].paddingTop).toBe(spacing.lg);
    expect(withHeaderStyle[1].paddingTop).toBe(spacing.sm);

    // A card with nothing for the header to say keeps the resting inset.
    const bare = FeedEntry({ story, onPress: () => undefined, showBottomHairline: false });
    const bareStyle = bare.props.style as readonly Record<string, unknown>[];
    expect(bareStyle[1]).toBeNull();
  });
});

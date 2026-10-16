import type { ImageLoadEventData, NativeSyntheticEvent } from 'react-native';

import { MAX_PORTRAIT_ASPECT_RATIO } from '../../utils/mediaFrame';
import AuthorLine from '../AuthorLine';
import FeedEntry from '../FeedEntry';
import FeedVideo from '../FeedVideo';
import PillarTag from '../PillarTag';

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
  Images: () => null,
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
    expect(authorLine?.props?.showRootedOnNewLine).toBe(true);
    expect(authorLine?.props?.size).toBe('large');
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

/** The first text element whose content matches the predicate, if any. */
function findTextElement(
  node: unknown,
  matches: (text: string) => boolean,
): { props?: Record<string, unknown> } | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findTextElement(child, matches);
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
  const children = element.props?.children;
  if (typeof children === 'string' && matches(children)) {
    return element as { props?: Record<string, unknown> };
  }
  if (element.props !== undefined) {
    return findTextElement(children, matches);
  }

  return undefined;
}

/** The fixture the version-variant assertions below are built from. */
const version = {
  ...story,
  id: 'version-9',
  title: 'Imvula yokuqala',
  preview: 'Ugogo wathi imvula yokuqala ikhumbula wonke amagama.',
  language: 'zul',
};

const ATTRIBUTION = "Adapted from Nomsa Khan's original story in English into Zulu.";

describe('FeedEntry version variant', () => {
  it('labels an adaptation with its version and language instead of the pillar tag', () => {
    const element = FeedEntry({
      story: version,
      variant: 'version',
      versionNumber: 2,
      attributionLine: ATTRIBUTION,
      onPress: () => undefined,
    });

    // The badge replaces the pillar tag: the pillar is the story's, inherited, so
    // repeating it would say nothing about this entry (KNOT-022).
    expect(elementTypes(element)).not.toContain(PillarTag);

    const text = collectText(element);
    expect(text).toContain('Version 2 · Zulu');
    // The overline voice upper-cases the label, so the wording is sentence case.
    expect(findTextElement(element, (value) => value === 'Version 2 · Zulu')?.props?.style).toEqual(
      expect.arrayContaining([expect.objectContaining({ textTransform: 'uppercase' })]),
    );
  });

  it('shows the attribution sentence verbatim, clamped to two lines', () => {
    const element = FeedEntry({
      story: version,
      variant: 'version',
      versionNumber: 3,
      attributionLine: ATTRIBUTION,
      onPress: () => undefined,
    });

    expect(collectText(element)).toContain(ATTRIBUTION);

    const line = findTextElement(element, (value) => value === ATTRIBUTION);
    expect(line?.props?.numberOfLines).toBe(2);
  });

  it('shows no attribution line when the server could not resolve one', () => {
    const element = FeedEntry({
      story: version,
      variant: 'version',
      versionNumber: 2,
      attributionLine: '',
      onPress: () => undefined,
    });

    expect(collectText(element)).not.toContain(ATTRIBUTION);
    // The badge still marks the entry, so a reader knows it is an adaptation.
    expect(collectText(element)).toContain('Version 2 · Zulu');
  });

  it('names the version without a number when the server sent none', () => {
    const element = FeedEntry({
      story: version,
      variant: 'version',
      attributionLine: ATTRIBUTION,
      onPress: () => undefined,
    });

    expect(collectText(element)).toContain('Version in Zulu');
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

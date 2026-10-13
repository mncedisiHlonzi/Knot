import Avatar from '../Avatar';
import AuthorLine from '../AuthorLine';
import Button from '../Button';
import Chip from '../Chip';
import EmptyState from '../EmptyState';
import FeedEntry from '../FeedEntry';
import FeedVideo from '../FeedVideo';
import PillarTag from '../PillarTag';
import RootedBadge from '../RootedBadge';
import TabBar from '../TabBar';
import { countLabel, feedMetaLine, totalReactions } from '../feedEntryMeta';

/**
 * These are the hookless components of the KNOT-020b library. The project has no
 * React Native Testing Library, and jest runs with `testEnvironment: 'node'`, so
 * the components are called directly as plain functions — the pattern the
 * AuthorLine suite already uses. Rendering through a renderer would need a new
 * dependency, which this task must not add. Stateful components (Input, Header,
 * ReactionBar) are covered by their pure logic instead.
 *
 * React Native is stubbed so its Flow source is not parsed, and `config/api` is
 * stubbed so the gitignored `secrets.local` module is never imported in CI
 * (KNOT-ADR-027).
 */
jest.mock('react-native', () => ({
  ActivityIndicator: 'ActivityIndicator',
  Image: 'Image',
  Pressable: 'Pressable',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

// Only the icons still used by a component under test: TabBar (the tab glyphs)
// and Chip (its remove affordance). ReactionBar's icons are the committed brand
// SVGs now, mapped to a stub by jest.config.js (KNOT-020b-fix).
jest.mock('phosphor-react-native', () => ({
  House: () => null,
  Images: () => null,
  MagnifyingGlass: () => null,
  MapTrifold: () => null,
  PlayCircle: () => null,
  Plus: () => null,
  SpeakerHigh: () => null,
  SpeakerSlash: () => null,
  User: () => null,
  X: () => null,
}));

// FeedEntry draws a video preview with react-native-video, which is a native
// module; stub it to a host component so the direct-call test can render the
// element (KNOT-020c-fix2).
jest.mock('react-native-video', () => ({ __esModule: true, default: 'Video' }));

jest.mock('../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));

describe('Button', () => {
  it('renders a primary button without invoking onPress', () => {
    const onPress = jest.fn();
    const element = Button({ label: 'Publish', onPress });

    expect(element).toBeTruthy();
    expect(onPress).not.toHaveBeenCalled();
  });

  it('renders every variant and the disabled and loading states', () => {
    const onPress = (): void => undefined;

    for (const variant of ['primary', 'secondary', 'ghost', 'danger'] as const) {
      expect(Button({ label: 'Go', onPress, variant })).toBeTruthy();
    }
    expect(Button({ label: 'Go', onPress, disabled: true })).toBeTruthy();
    expect(Button({ label: 'Go', onPress, loading: true })).toBeTruthy();
    expect(Button({ label: 'Go', onPress, fullWidth: true })).toBeTruthy();
  });
});

describe('Chip', () => {
  it('renders the default, outline and selected presentations', () => {
    expect(Chip({ label: 'Pillar' })).toBeTruthy();
    expect(Chip({ label: 'Pillar', variant: 'outline' })).toBeTruthy();
    expect(Chip({ label: 'Pillar', selected: true })).toBeTruthy();
  });

  it('renders a control when onPress is given, and one with a remove affordance', () => {
    const onPress = jest.fn();
    const onRemove = jest.fn();

    expect(Chip({ label: 'Wonder', onPress })).toBeTruthy();
    expect(Chip({ label: 'Wonder', onPress, onRemove })).toBeTruthy();
    expect(onPress).not.toHaveBeenCalled();
    expect(onRemove).not.toHaveBeenCalled();
  });
});

describe('Avatar', () => {
  it('draws an image when a uri is given, and initials otherwise (never both)', () => {
    const withImage = Avatar({ uri: 'https://example.test/a.png', displayName: 'Ada Lovelace' });
    const withInitials = Avatar({ displayName: 'Ada Lovelace' });
    const blankUri = Avatar({ uri: '   ', displayName: 'Ada Lovelace' });

    expect(withImage.props.children.type).toBe('Image');
    expect(withInitials.props.children.type).toBe('View');
    expect(blankUri.props.children.type).toBe('View');
  });

  it('renders every size and accepts an onPress without invoking it', () => {
    const onPress = jest.fn();

    for (const size of ['xs', 'sm', 'md', 'lg', 'xl'] as const) {
      expect(Avatar({ displayName: 'Ada', size })).toBeTruthy();
    }
    expect(Avatar({ displayName: 'Ada', onPress })).toBeTruthy();
    expect(onPress).not.toHaveBeenCalled();
  });

  it('draws the Feed avatar (lg) at 48 points', () => {
    // The Feed's author block uses AuthorLine size="large", which maps to the
    // Avatar's `lg` (KNOT-020c-fix5).
    const fallback = Avatar({ displayName: 'Ada Lovelace', size: 'lg' }).props.children;
    expect(fallback.props.style[1]).toEqual({ height: 48, width: 48 });
  });
});

describe('PillarTag', () => {
  it('renders both pillars, with and without a place', () => {
    expect(PillarTag({ pillar: 'wonder' })).toBeTruthy();
    expect(PillarTag({ pillar: 'heritage' })).toBeTruthy();
    expect(PillarTag({ pillar: 'heritage', place: 'Oaxaca, Mexico' })).toBeTruthy();
    expect(PillarTag({ pillar: 'wonder', place: '   ' })).toBeTruthy();
  });
});

describe('RootedBadge', () => {
  it('renders a place and, in the full form, a duration', () => {
    expect(RootedBadge({ place: 'Oaxaca', durationBucket: 'many_years' })).toBeTruthy();
    expect(
      RootedBadge({ place: 'Oaxaca', durationBucket: 'many_years', compact: true }),
    ).toBeTruthy();
  });

  it('renders nothing for a blank place', () => {
    expect(RootedBadge({ place: '   ', durationBucket: 'recently' })).toBeNull();
  });

  // The Feed's quiet line drops the duration (KNOT-020c-fix5), so it reads
  // "Rooted · {place}" while the badge keeps the full form.
  it('renders the quiet line with, and without, the duration', () => {
    const full = RootedBadge({ place: 'Manguzi', durationBucket: 'lifelong', variant: 'quiet' });
    const short = RootedBadge({
      place: 'Manguzi',
      durationBucket: 'lifelong',
      variant: 'quiet',
      showDuration: false,
    });

    expect(full?.props.children.props.children).toBe('Rooted · Manguzi · Lifelong');
    expect(short?.props.children.props.children).toBe('Rooted · Manguzi');
    expect(short?.props.accessibilityLabel).toBe('Rooted in Manguzi');
  });

  it('does not throw on a non-string place or bucket', () => {
    expect(() =>
      RootedBadge({ place: 42 as unknown as string, durationBucket: 'recently' }),
    ).not.toThrow();
    expect(() =>
      RootedBadge({ place: 'Oaxaca', durationBucket: 7 as unknown as string }),
    ).not.toThrow();
  });
});

describe('EmptyState', () => {
  it('renders a headline, and optional body and action', () => {
    expect(EmptyState({ headline: 'Nothing here yet' })).toBeTruthy();
    expect(EmptyState({ headline: 'Empty', body: 'Try again later' })).toBeTruthy();

    const onPress = jest.fn();
    expect(EmptyState({ headline: 'Empty', action: { label: 'Retry', onPress } })).toBeTruthy();
    expect(onPress).not.toHaveBeenCalled();
  });

  it('draws a named brand illustration instead of the placeholder', () => {
    const placeholder = EmptyState({ headline: 'Empty' });
    expect(placeholder.props.children[0].props.children.type).toBe('View');

    const named = EmptyState({ headline: 'Empty', illustrationName: 'no-stories' });
    expect(named.props.children[0].props.children.type).not.toBe('View');
  });

  it('lets a caller-supplied illustration node take precedence over the name', () => {
    const element = EmptyState({
      headline: 'Empty',
      illustrationName: 'no-results',
      illustration: 'CUSTOM',
    });
    expect(element.props.children[0].props.children).toBe('CUSTOM');
  });
});

describe('TabBar', () => {
  it('renders five tabs and never fires onSelect during render', () => {
    const onSelect = jest.fn();
    const element = TabBar({ activeTab: 'feed', onSelect });

    expect(element.props.children).toHaveLength(5);
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('renders with each tab active', () => {
    const onSelect = (): void => undefined;
    for (const tab of ['feed', 'discoveryMap', 'createStory', 'inquiries', 'profile'] as const) {
      expect(TabBar({ activeTab: tab, onSelect })).toBeTruthy();
    }
  });
});

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

describe('FeedEntry', () => {
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
});

describe('feedMetaLine helper', () => {
  it('pluralises a count', () => {
    expect(countLabel(1, 'version', 'versions')).toBe('1 version');
    expect(countLabel(4, 'version', 'versions')).toBe('4 versions');
    expect(countLabel(1, 'language', 'languages')).toBe('1 language');
    expect(countLabel(2, 'comment', 'comments')).toBe('2 comments');
    expect(countLabel(1, 'reaction', 'reactions')).toBe('1 reaction');
    expect(countLabel(2, 'reaction', 'reactions')).toBe('2 reactions');
  });

  it('sums the four signals', () => {
    expect(
      totalReactions({
        rings_true: 2,
        know_it_differently: 3,
        adds_something_new: 4,
        needs_a_source: 5,
      }),
    ).toBe(14);
  });

  it('renders the counts as one text line, in order', () => {
    expect(
      feedMetaLine({
        versionCount: 4,
        languageCount: 3,
        commentCount: 14,
        reactions: {
          rings_true: 128,
          know_it_differently: 19,
          adds_something_new: 7,
          needs_a_source: 2,
        },
      }),
    ).toBe('4 versions · 3 languages · 156 reactions · 14 comments');
  });

  it('spells the reaction count out in words rather than drawing a glyph', () => {
    expect(
      feedMetaLine({
        versionCount: 1,
        languageCount: 1,
        commentCount: 1,
        reactions: {
          rings_true: 1,
          know_it_differently: 0,
          adds_something_new: 0,
          needs_a_source: 0,
        },
      }),
    ).toBe('1 version · 1 language · 1 reaction · 1 comment');
  });

  it('omits zero counts and returns an empty line when everything is zero', () => {
    expect(feedMetaLine({ versionCount: 0, languageCount: 0, commentCount: 0 })).toBe('');
    expect(feedMetaLine({})).toBe('');
    expect(feedMetaLine({ versionCount: 1 })).toBe('1 version');
  });
});

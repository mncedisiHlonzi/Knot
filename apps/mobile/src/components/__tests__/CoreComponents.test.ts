import Avatar from '../Avatar';
import Button from '../Button';
import Chip from '../Chip';
import EmptyState from '../EmptyState';
import PillarTag from '../PillarTag';
import RootedBadge from '../RootedBadge';
import TabBar from '../TabBar';
import { ChatsCircle } from 'phosphor-react-native';
import {
  attributionParts,
  attributionSegments,
  countLabel,
  feedMetaLine,
  isPreviewTruncated,
  totalReactions,
} from '../feedEntryMeta';

/**
 * These are the hookless components of the KNOT-020b library. The project has no
 * React Native Testing Library, and jest runs with `testEnvironment: 'node'`, so
 * the components are called directly as plain functions — the pattern the
 * AuthorLine suite already uses. Rendering through a renderer would need a new
 * dependency, which this task must not add. Stateful components (Input, Header,
 * ReactionBar) are covered by their pure logic instead, and `FeedEntry` — which
 * now holds state for its media frame — has its own suite that stubs the hook
 * primitives (KNOT-020g-fix7).
 *
 * `Button` and `Chip` gained animation state in KNOT-020h (a press scale and a
 * fill/icon transition), so the hook primitives are stubbed here too. React
 * Native is stubbed so its Flow source is not parsed, and `config/api` is
 * stubbed so the gitignored `secrets.local` module is never imported in CI
 * (KNOT-ADR-027).
 */
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  useCallback: (callback: unknown) => callback,
  useEffect: () => undefined,
  useRef: (initial: unknown) => ({ current: initial }),
  // Components read the active theme through the context, and there is no React
  // dispatcher outside a render. Supply the provider's own fallback value: the
  // light theme with no-op controls (KNOT-020i).
  useContext: () => jest.requireActual('../../theme/ThemeContext').lightTheme,
}));

jest.mock('react-native', () => ({
  ActivityIndicator: 'ActivityIndicator',
  // The motion pass (KNOT-020h) gave Button and Chip animated values. The
  // animations themselves are visual and are not asserted; the stand-ins only
  // have to exist so a direct call can render, exactly as the app would.
  Animated: {
    Value: class {
      setValue(): void {}
      interpolate(): number {
        return 0;
      }
    },
    View: 'Animated.View',
    createAnimatedComponent: <T>(component: T): T => component,
    loop: () => ({ start: () => undefined, stop: () => undefined }),
    spring: () => ({ start: () => undefined, stop: () => undefined }),
    timing: () => ({ start: () => undefined, stop: () => undefined }),
  },
  Easing: { bezier: () => (progress: number) => progress },
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
  Bell: () => null,
  ChatsCircle: () => null,
  House: () => null,
  MapTrifold: () => null,
  Plus: () => null,
  X: () => null,
}));

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

  // Item 1 (KNOT-020c-fix6): the plain variant must reset the pill's horizontal
  // padding, so the tag sits flush with the avatar and the story text rather than
  // starting a step in. There is no renderer, so the style array is inspected
  // directly: the base style carries the pill's padding and the plain override
  // zeroes it.
  it('resets the horizontal padding in the plain variant', () => {
    const plain = PillarTag({ pillar: 'wonder', variant: 'plain' }) as unknown as {
      props: { style: readonly Record<string, unknown>[] };
    };
    const applied = plain.props.style;

    expect(applied[0]).toHaveProperty('paddingHorizontal');
    expect(applied[applied.length - 1]).toMatchObject({ paddingHorizontal: 0 });
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

  // The fifth destination is Notifications, and Profile left the bar for the
  // Feed header's avatar (KNOT-020j).
  it('renders with each tab active', () => {
    const onSelect = (): void => undefined;
    for (const tab of [
      'feed',
      'discoveryMap',
      'createStory',
      'inquiries',
      'notifications',
    ] as const) {
      expect(TabBar({ activeTab: tab, onSelect })).toBeTruthy();
    }
  });

  it('draws the unread badge only when there is an unread count', () => {
    const onSelect = (): void => undefined;

    /** The badge is the notification glyph box's second child. */
    const badgeFor = (unreadCount: number): unknown => {
      const bar = TabBar({ activeTab: 'feed', onSelect, unreadCount });
      const notificationsTab = bar.props.children[4] as {
        props: { children: readonly { props: { children: readonly unknown[] } }[] };
      };
      const glyph = notificationsTab.props.children[0];
      return glyph.props.children[1];
    };

    expect(badgeFor(0)).toBeNull();
    expect(badgeFor(3)).not.toBeNull();
  });

  // The visible label is shortened so it cannot truncate on a narrow device; the
  // spoken label stays the full destination name (KNOT-020j-fix).
  it('draws "Notice" for the notifications tab but announces "Notifications"', () => {
    const onSelect = (): void => undefined;
    const bar = TabBar({ activeTab: 'feed', onSelect });
    const notificationsTab = bar.props.children[4] as {
      props: { accessibilityLabel: string; children: readonly { props: { children: unknown } }[] };
    };

    expect(notificationsTab.props.children[1].props.children).toBe('Notice');
    expect(notificationsTab.props.accessibilityLabel).toBe('Notifications');
  });

  // The magnifier is the Feed header's search control, so Inquiries needed its own
  // mark rather than looking like a second search action (KNOT-020j-fix).
  it('draws a conversation glyph, not a magnifier, for Inquiries', () => {
    const onSelect = (): void => undefined;
    const bar = TabBar({ activeTab: 'feed', onSelect });
    const inquiriesTab = bar.props.children[3] as {
      props: {
        children: readonly { props: { children: readonly { props: { icon: unknown } }[] } }[];
      };
    };

    const glyph = inquiriesTab.props.children[0].props.children[0];
    expect(glyph.props.icon).toBe(ChatsCircle);
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

describe('isPreviewTruncated helper', () => {
  it('reads the excerpt server-side: the ellipsis is the mark that there is more', () => {
    expect(isPreviewTruncated('Every morning she tied a knot in a cord…')).toBe(true);
    // Trailing whitespace does not hide the mark.
    expect(isPreviewTruncated('A cut excerpt… ')).toBe(true);
  });

  it('treats a whole body as untruncated', () => {
    expect(isPreviewTruncated('A whole body.')).toBe(false);
    expect(isPreviewTruncated('')).toBe(false);
  });

  it('does not mistake a mid-text ellipsis for a truncation mark', () => {
    expect(isPreviewTruncated('He paused… then went on.')).toBe(false);
  });
});

describe('attributionParts helper', () => {
  it('splits the adapted form around the name', () => {
    expect(
      attributionParts("Adapted from Ada Lovelace's original story in English into Zulu."),
    ).toEqual({
      prefix: 'Adapted from ',
      name: 'Ada Lovelace',
      suffix: "'s original story in English into Zulu.",
    });
  });

  it('splits the original-story form around the name', () => {
    expect(attributionParts('Original story by Ada Lovelace in English.')).toEqual({
      prefix: 'Original story by ',
      name: 'Ada Lovelace',
      suffix: ' in English.',
    });
  });

  it('takes the FIRST name in a deep adaptation, not a later one', () => {
    // "Adapted from Bongani's Zulu version of Ada's original story..." opens with
    // the immediate parent; a greedy match would swallow the whole sentence.
    expect(
      attributionParts(
        "Adapted from Bongani Ncube's Zulu version of Ada Lovelace's original story in English into Swati.",
      )?.name,
    ).toBe('Bongani Ncube');
  });

  it('returns undefined for an unexpected shape or blank input', () => {
    expect(attributionParts('Adapted from someone.')).toBeUndefined();
    expect(attributionParts('')).toBeUndefined();
    expect(attributionParts('   ')).toBeUndefined();
  });
});

describe('attributionSegments helper', () => {
  it('bolds the name in the adapted form', () => {
    expect(
      attributionSegments("Adapted from Ada Lovelace's original story in English into Zulu."),
    ).toEqual([
      { text: 'Adapted from ', bold: false },
      { text: 'Ada Lovelace', bold: true },
      { text: "'s original story in English into Zulu.", bold: false },
    ]);
  });

  it('bolds the name in the original-story form', () => {
    expect(attributionSegments('Original story by Ada Lovelace in English.')).toEqual([
      { text: 'Original story by ', bold: false },
      { text: 'Ada Lovelace', bold: true },
      { text: ' in English.', bold: false },
    ]);
  });

  it('bolds both names in a three-deep lineage', () => {
    expect(
      attributionSegments(
        "Adapted from Bongani Ncube's Zulu version of Ada Lovelace's original story in English into Swati.",
      ),
    ).toEqual([
      { text: 'Adapted from ', bold: false },
      { text: 'Bongani Ncube', bold: true },
      { text: "'s Zulu version of ", bold: false },
      { text: 'Ada Lovelace', bold: true },
      { text: "'s original story in English into Swati.", bold: false },
    ]);
  });

  it('finds the original author after "to" in the long lineage form', () => {
    const segments = attributionSegments(
      "Adapted from Bongani Ncube's Zulu version, which traces back through 2 adaptations to Ada Lovelace's original story in English, into Swati.",
    );

    expect(segments.filter((segment) => segment.bold).map((segment) => segment.text)).toEqual([
      'Bongani Ncube',
      'Ada Lovelace',
    ]);
  });

  it('returns the whole sentence as one plain run for an unexpected shape', () => {
    expect(attributionSegments('Adapted from someone.')).toEqual([
      { text: 'Adapted from someone.', bold: false },
    ]);
  });

  it('returns no runs for blank input', () => {
    expect(attributionSegments('')).toEqual([]);
    expect(attributionSegments('   ')).toEqual([]);
  });

  it('bolds the adapter and the original in the new adapted-by phrasing', () => {
    expect(
      attributionSegments("Adapted by Sipho Dlamini from Nomsa Khan's original story in English."),
    ).toEqual([
      { text: 'Adapted by ', bold: false },
      { text: 'Sipho Dlamini', bold: true },
      { text: ' from ', bold: false },
      { text: 'Nomsa Khan', bold: true },
      { text: "'s original story in English.", bold: false },
    ]);
  });

  it('bolds the adapter, parent, and original in a three-deep lineage', () => {
    expect(
      attributionSegments(
        "Adapted by Thandi Mokoena from Sipho Dlamini's Zulu version of Nomsa Khan's original story in English.",
      ),
    ).toEqual([
      { text: 'Adapted by ', bold: false },
      { text: 'Thandi Mokoena', bold: true },
      { text: ' from ', bold: false },
      { text: 'Sipho Dlamini', bold: true },
      { text: "'s Zulu version of ", bold: false },
      { text: 'Nomsa Khan', bold: true },
      { text: "'s original story in English.", bold: false },
    ]);
  });

  it('bolds the adapter, parent, and original in the long lineage form', () => {
    expect(
      attributionSegments(
        "Adapted by Ana Ruiz from Thandi Mokoena's Xhosa version, tracing back to Nomsa Khan's original story.",
      ),
    ).toEqual([
      { text: 'Adapted by ', bold: false },
      { text: 'Ana Ruiz', bold: true },
      { text: ' from ', bold: false },
      { text: 'Thandi Mokoena', bold: true },
      { text: "'s Xhosa version, tracing back to ", bold: false },
      { text: 'Nomsa Khan', bold: true },
      { text: "'s original story.", bold: false },
    ]);
  });
});

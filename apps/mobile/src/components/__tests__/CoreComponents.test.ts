import Avatar from '../Avatar';
import Button from '../Button';
import Chip from '../Chip';
import EmptyState from '../EmptyState';
import PillarTag from '../PillarTag';
import RootedBadge from '../RootedBadge';
import TabBar, { type TabName } from '../TabBar';
import { BellIcon, ChatIcon, HomeIcon, MapIcon } from '../TabIcons';
import { fontWeights } from '../../theme';
import { colors } from '../../theme';
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
  // Button and Chip animate through the shared press-scale primitive (KNOT-020k),
  // which reads a React Native accessibility module this node-environment suite
  // does not load. The primitive has its own suite; here it is a stand-in so a
  // direct call can render.
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

// Only the icon still used by a component under test: Chip's remove affordance.
// ReactionBar's icons are the committed brand SVGs now, mapped to a stub by
// jest.config.js (KNOT-020b-fix). The bottom bar's Ionicons are stubbed below.
jest.mock('../../motion/usePressScale', () => ({
  usePressScale: () => ({ scale: 1, onPressIn: () => undefined, onPressOut: () => undefined }),
}));

jest.mock('phosphor-react-native', () => ({
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

  // KNOT-020k: the pillar is bold text only — no overline, no underline, no
  // letter-spacing, no mark, no pill, no background and no border.
  it('renders the pillar and place as one bold primary-ink line', () => {
    const element = PillarTag({ pillar: 'heritage', place: 'Oaxaca' }) as unknown as {
      type: unknown;
      props: { children: unknown; style: Record<string, unknown> };
    };

    expect(element.props.children).toBe('Heritage · Oaxaca');
    expect(Array.isArray(element.props.style)).toBe(false);
    expect(element.props.style).toMatchObject({
      color: colors.text.primary,
      fontWeight: '700',
    });
    expect(element.props.style).not.toHaveProperty('textTransform');
    expect(element.props.style).not.toHaveProperty('textDecorationLine');
    // The caption composite's tracking is explicitly cancelled: no letter-spacing.
    expect(element.props.style.letterSpacing).toBe(0);
    expect(element.props.style).not.toHaveProperty('backgroundColor');
    expect(element.props.style).not.toHaveProperty('borderWidth');
  });

  it('renders only the pillar when there is no place', () => {
    const element = PillarTag({ pillar: 'wonder', place: '   ' }) as unknown as {
      props: { children: unknown };
    };

    expect(element.props.children).toBe('Wonder');
  });
});

/**
 * The style array of every `Text` in a directly-called component's tree — enough
 * to prove a weight rule without a renderer (KNOT-020k-fix24).
 */
function textStyles(node: unknown, found: unknown[] = []): unknown[] {
  if (Array.isArray(node)) {
    for (const child of node) {
      textStyles(child, found);
    }
    return found;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return found;
  }

  const element = node as { type?: unknown; props?: { children?: unknown; style?: unknown } };
  if (element.type === 'Text' && element.props?.style !== undefined) {
    found.push(element.props.style);
  }
  if (element.props !== undefined) {
    textStyles(element.props.children, found);
  }

  return found;
}

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

  // The pill is text only: the Rooted mark was removed so the label itself carries
  // the emphasis (KNOT-020k-fix24).
  it('draws no mark, and sets the label in the bold weight', () => {
    const badge = RootedBadge({ place: 'Oaxaca', durationBucket: 'many_years' });

    // One child — the text row — where the mark used to sit beside it.
    expect(Array.isArray(badge?.props.children)).toBe(false);

    const styles = textStyles(badge);
    expect(styles.length).toBeGreaterThan(0);
    for (const style of styles) {
      expect(style).toEqual(
        expect.arrayContaining([expect.objectContaining({ fontWeight: fontWeights.bold })]),
      );
    }
  });

  it('gives the quiet line the same bold weight', () => {
    const line = RootedBadge({
      place: 'Manguzi',
      durationBucket: 'lifelong',
      variant: 'quiet',
      showDuration: false,
    });
    const styles = textStyles(line);

    expect(styles).toHaveLength(1);
    expect(styles[0]).toEqual(
      expect.arrayContaining([expect.objectContaining({ fontWeight: fontWeights.bold })]),
    );
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
  type TabIconElement = {
    type: unknown;
    props: { size: number; color: string; isActive: boolean };
  };

  /**
   * The SVG-asset element a tab draws, read by invoking the glyph component with
   * its own props — the same direct-call approach the rest of this suite uses,
   * since there is no renderer. A tab is now a glyph box plus a label, so the
   * glyph is the box's only child.
   */
  const iconFor = (activeTab: TabName, index: number): TabIconElement => {
    const bar = TabBar({ activeTab, onSelect: () => undefined });
    const tab = bar.props.children[index] as { props: { children: readonly unknown[] } };
    // The box holds the bouncing glyph, plus the unread badge on Notifications.
    const [glyphBox] = tab.props.children as readonly [{ props: { children: unknown } }];
    const slot = glyphBox.props.children;
    const glyph = (Array.isArray(slot) ? slot[0] : slot) as {
      type: (props: unknown) => { props: { children: unknown } };
      props: unknown;
    };

    return glyph.type(glyph.props).props.children as TabIconElement;
  };

  /** The name drawn under one tab's glyph. */
  const labelFor = (index: number): string => {
    const bar = TabBar({ activeTab: 'feed', onSelect: () => undefined });
    const tab = bar.props.children[index] as { props: { children: readonly unknown[] } };
    const [, label] = tab.props.children as readonly [unknown, { props: { children: string } }];

    return label.props.children;
  };

  /**
   * The style array of one tab's label, so ink and weight can be read without a
   * renderer.
   */
  const labelStyle = (index: number): readonly ({ color?: string } | null)[] => {
    const bar = TabBar({ activeTab: 'feed', onSelect: () => undefined });
    const tab = bar.props.children[index] as { props: { children: readonly unknown[] } };
    const [, label] = tab.props.children as readonly [
      unknown,
      { props: { style: readonly ({ color?: string } | null)[] } },
    ];

    return label.props.style;
  };

  it('renders four tabs and never fires onSelect during render', () => {
    const onSelect = jest.fn();
    const element = TabBar({ activeTab: 'feed', onSelect });

    expect(element.props.children).toHaveLength(4);
    expect(onSelect).not.toHaveBeenCalled();
  });

  // Create left the bar for the composer row at the top of the feed, which leaves
  // the four destinations sharing the width evenly (KNOT-020k-fix6).
  it('renders with each tab active', () => {
    const onSelect = (): void => undefined;
    for (const tab of ['feed', 'discoveryMap', 'inquiries', 'notifications'] as const) {
      expect(TabBar({ activeTab: tab, onSelect })).toBeTruthy();
    }
  });

  it('draws the unread badge only when there is an unread count', () => {
    const onSelect = (): void => undefined;

    /** The badge is the notification glyph box's second child. */
    const badgeFor = (unreadCount: number): unknown => {
      const bar = TabBar({ activeTab: 'feed', onSelect, unreadCount });
      const notificationsTab = bar.props.children[3] as {
        props: { children: readonly { props: { children: readonly unknown[] } }[] };
      };
      const [glyphBox] = notificationsTab.props.children;
      return glyphBox.props.children[1];
    };

    expect(badgeFor(0)).toBeNull();
    expect(badgeFor(3)).not.toBeNull();
  });

  // Labels are back (KNOT-020k-fix6): a custom glyph set needs its names, so each
  // tab draws one under its artwork and keeps the spoken label on the tab itself.
  it('draws the destination name under every glyph, in the tab ink', () => {
    expect([0, 1, 2, 3].map((index) => labelFor(index))).toEqual([
      'Home',
      'Map',
      'Inquiries',
      'Notifications',
    ]);

    const onSelect = (): void => undefined;
    const bar = TabBar({ activeTab: 'feed', onSelect });
    const tabs = bar.props.children as readonly {
      props: { accessibilityLabel: string; children: readonly unknown[] };
    }[];

    expect(tabs).toHaveLength(4);
    expect(tabs[0]?.props.accessibilityLabel).toBe('Home');
    expect(tabs[1]?.props.accessibilityLabel).toBe('Map');
    expect(tabs[2]?.props.accessibilityLabel).toBe('Inquiries');
    expect(tabs[3]?.props.accessibilityLabel).toBe('Notifications');

    // The active tab's label takes the primary ink; an idle one the tertiary ink
    // with no override on top of it.
    expect(labelStyle(0)[1]).toEqual(expect.objectContaining({ color: colors.text.tertiary }));
    expect(labelStyle(0)[2]).toEqual({ color: colors.text.primary });
    expect(labelStyle(1)[2]).toBeNull();
  });

  // The bar draws the founder's own SVG files through one component per
  // destination, in bar order, so no icon library is involved (KNOT-020k-fix5).
  it('draws a custom SVG glyph for every destination', () => {
    expect(iconFor('feed', 0).type).toBe(HomeIcon);
    expect(iconFor('feed', 1).type).toBe(MapIcon);
    expect(iconFor('feed', 2).type).toBe(ChatIcon);
    expect(iconFor('feed', 3).type).toBe(BellIcon);
  });

  // The selected destination draws the solid cut in the primary ink and every
  // other tab the outline cut in the tertiary ink: the state is a prop, and each
  // component decides which asset that means (KNOT-020k-fix5).
  it('marks the selected destination active and inks it primary', () => {
    const selected = iconFor('discoveryMap', 1);
    expect(selected.props.isActive).toBe(true);
    expect(selected.props.color).toBe(colors.text.primary);

    const idle = iconFor('notifications', 0);
    expect(idle.props.isActive).toBe(false);
    expect(idle.props.color).toBe(colors.text.tertiary);

    // 24pt in every state, a step down from 28 so the label fits under it
    // (KNOT-020k-fix6).
    expect(idle.props.size).toBe(24);
    expect(selected.props.size).toBe(24);
  });

  // Exactly one tab is ever active, so the two-tone rule never shows two solid
  // glyphs at once (KNOT-020k-fix5).
  it('never marks more than one destination active', () => {
    const destinations: readonly (readonly [TabName, number])[] = [
      ['feed', 0],
      ['discoveryMap', 1],
      ['inquiries', 2],
      ['notifications', 3],
    ];

    for (const [activeTab, activeIndex] of destinations) {
      const activeIndexes = destinations
        .map(([, index]) => index)
        .filter((index) => iconFor(activeTab, index).props.isActive);

      expect(activeIndexes).toEqual([activeIndex]);
    }
  });

  // The asset receives the bar's box and ink verbatim; the 512-unit grid and the
  // `currentColor` fill live in the SVG file itself (KNOT-020k-fix5).
  it('invokes a destination glyph into a 24pt SVG in the idle ink', () => {
    const glyph = iconFor('notifications', 0);
    const svg = (
      glyph.type as (props: unknown) => { type: unknown; props: Record<string, unknown> }
    )(glyph.props);

    expect(svg.props.width).toBe(24);
    expect(svg.props.height).toBe(24);
    expect(svg.props.color).toBe(colors.text.tertiary);
    expect(svg.props.testID).toBe('tab-icon-home-outline');
  });

  // The destination that is selected draws the `*-fill` asset and every other tab
  // the `*-outline` one, so the bar's two-tone rule comes from the founder's own
  // files rather than from hand-drawn paths (KNOT-020k-fix5).
  it('draws the fill asset for the selected destination', () => {
    const glyph = iconFor('discoveryMap', 1);
    const svg = (
      glyph.type as (props: unknown) => { type: unknown; props: Record<string, unknown> }
    )(glyph.props);

    expect(svg.props.testID).toBe('tab-icon-map-fill');
    expect(svg.props.color).toBe(colors.text.primary);
    expect(svg.props.width).toBe(24);
  });

  // The magnifier is the Feed header's search control, so Inquiries needed its own
  // mark rather than looking like a second search action (KNOT-020j-fix).
  it('draws a conversation glyph for Inquiries', () => {
    expect(iconFor('notifications', 2).type).toBe(ChatIcon);
    expect(iconFor('inquiries', 2).props.isActive).toBe(true);
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

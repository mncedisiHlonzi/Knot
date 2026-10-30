import React, { useEffect, useRef } from 'react';
import { Animated, Easing, Pressable, StyleSheet, Text, View } from 'react-native';

import {
  motion,
  radius,
  spacing,
  tabularNums,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../theme';
import type { ThemeColors } from '../theme';
import { BellIcon, ChatIcon, HomeIcon, MapIcon, PlusIcon } from './TabIcons';
import type { TabIconProps } from './TabIcons';

/** The five primary destinations of the app. */
export type TabName = 'feed' | 'discoveryMap' | 'createStory' | 'inquiries' | 'notifications';

/**
 * A tab glyph. Every one of the five is the founder's own SVG asset, rendered by
 * `TabIcons` rather than borrowed from an icon library (KNOT-020k-fix5): a tab's
 * state is a prop (`isActive`) rather than a second glyph component, because one
 * component owns both cuts of its own shape.
 */
type TabIcon = React.ComponentType<TabIconProps>;

/** One tab's definition: where it goes, its spoken name, and the glyph it draws. */
type TabDefinition = {
  readonly name: TabName;
  /**
   * The destination's name. It is never drawn — the bar is text free — and is read
   * out by a screen reader in its place (KNOT-020k-fix).
   */
  readonly label: string;
  /** The destination's SVG asset, drawn through `TabIcons`. */
  readonly icon: TabIcon;
  /** The spoken label, when it differs from the visible one. */
  readonly accessibilityLabel?: string;
};

/** The tabs, in the order they appear left to right. */
const TABS: readonly TabDefinition[] = [
  { name: 'feed', label: 'Home', icon: HomeIcon },
  { name: 'discoveryMap', label: 'Map', icon: MapIcon },
  // The Create tab is the elevated centre button; its icon is the only thing drawn.
  {
    name: 'createStory',
    label: 'Create',
    icon: PlusIcon,
    accessibilityLabel: 'Create story',
  },
  // Inquiries has its own mark: a magnifier was the same glyph as the Feed
  // header's search control, which made two different destinations look like one
  // action. A speech bubble reads as an exchange of questions and answers, and is
  // clearly a destination rather than a control (KNOT-020j-fix).
  { name: 'inquiries', label: 'Inquiries', icon: ChatIcon },
  // The spoken label is the full destination name: the bar draws no text at all,
  // so a screen reader is the only place the name is needed (KNOT-020j-fix,
  // KNOT-020k-fix).
  { name: 'notifications', label: 'Notifications', icon: BellIcon },
];

type TabBarProps = {
  /** The tab currently shown. */
  readonly activeTab: TabName;
  /** Called when a tab is tapped. */
  readonly onSelect: (tab: TabName) => void;
  /**
   * The number of unread notifications, drawn as a badge on the Notifications
   * tab. The app owns the count (it reads it when the bar is shown and whenever
   * the active tab changes), so the bar itself stays stateless. Defaults to 0,
   * which draws no badge (KNOT-020j).
   */
  readonly unreadCount?: number;
};

/** The bar's height. The bottom safe-area inset is inherited from the app root. */
const BAR_HEIGHT = 64;

/**
 * The icon size drawn in each tab.
 *
 * 28 points, the size the bar has used since KNOT-020k-fix3. The supplied artwork
 * is drawn on a 512-unit grid, so it scales cleanly to any box; 28pt is what keeps
 * five glyphs and the elevated Create disc inside the 64pt bar (KNOT-020k-fix5).
 */
const ICON_SIZE = 28;

/** The diameter of the elevated Create button. */
const CREATE_BUTTON_SIZE = 48;

/**
 * How far the selected tab's glyph swells at the peak of its bounce, and how
 * much smaller it starts. Both are small enough to read as a settle rather than
 * as a bounce of its own (KNOT-020h).
 */
const BOUNCE_PEAK = 1.12;
const BOUNCE_START = 0.9;

/**
 * One tab's glyph, with the selection bounce.
 *
 * It is a component of its own so the bounce's animated value is per tab and the
 * bar itself stays stateless — the value only moves when its own tab becomes the
 * selected one. Both steps use the motion tokens' `micro` duration, so the
 * bounce never reads as a delay between the tap and the destination.
 */
function TabGlyph({
  icon: Icon,
  color,
  active,
}: {
  readonly icon: TabIcon;
  readonly color: string;
  readonly active: boolean;
}): React.ReactElement {
  const scale = useRef(new Animated.Value(1)).current;

  useEffect(() => {
    if (!active) {
      scale.setValue(1);
      return undefined;
    }

    scale.setValue(BOUNCE_START);
    const bounce = Animated.sequence([
      Animated.timing(scale, {
        toValue: BOUNCE_PEAK,
        duration: motion.durations.micro,
        easing: Easing.bezier(...motion.easing.standard),
        useNativeDriver: true,
      }),
      Animated.timing(scale, {
        toValue: 1,
        duration: motion.durations.micro,
        easing: Easing.bezier(...motion.easing.exit),
        useNativeDriver: true,
      }),
    ]);
    bounce.start();

    return () => bounce.stop();
  }, [active, scale]);

  return (
    <Animated.View style={{ transform: [{ scale }] }}>
      <Icon size={ICON_SIZE} color={color} isActive={active} />
    </Animated.View>
  );
}

/**
 * The hand-rolled bottom tab bar: five destinations, no library.
 *
 * It is a plain row of buttons over the active screen. Non-tab screens (a story,
 * a question, a place, the composer's overlays) are pushed over the bar rather
 * than shown inside it, so the bar is only ever rendered for a primary
 * destination. Using no navigation dependency is deliberate; see KNOT-ADR-019.
 *
 * The five destinations are Home, Map, Create, Inquiries, and Notifications
 * (KNOT-020j). Inquiries is a destination in its own right — finding and asking
 * questions — rather than something reached from Create, which stays a story
 * composer (KNOT-ADR-058). Notifications took the fifth slot, and the bell left
 * the Feed's header, so the inbox has a permanent, always-reachable home; the
 * profile it displaced is now the Feed header's avatar.
 *
 * The bar is **text free** (KNOT-020k-fix): no label is drawn under any glyph, so
 * the icons sit on the bar's centre line and each tab keeps only a spoken name for
 * a screen reader. The glyphs are the founder's own SVGs (KNOT-020k-fix5): the
 * selected tab draws its solid (`*-fill`) cut in the primary ink and the rest draw
 * the outline cut in the tertiary ink — a two-tone rule that reads without relying
 * on colour alone. The Create tab stays an elevated disc, sunken at rest and filled
 * in the brand ink when it is the active tab, with the plus drawn in the disc's own
 * ink in both states (the set has one plus, and the disc behind it already carries
 * the selection). The bottom safe-area inset is not applied here: the bar already
 * sits inside the app root's `SafeAreaView` (KNOT-020b).
 */
export default function TabBar({
  activeTab,
  onSelect,
  unreadCount = 0,
}: TabBarProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  return (
    <View style={styles.bar} accessibilityRole="tablist">
      {TABS.map((tab) => {
        const Icon = tab.icon;
        const active = tab.name === activeTab;
        const unread = tab.name === 'notifications' ? unreadCount : 0;
        const label =
          tab.name === 'notifications' && unread > 0
            ? `Notifications, ${unread} unread`
            : (tab.accessibilityLabel ?? tab.label);

        // The Create tab is the elevated centre button; its icon is the only thing
        // drawn. The plus keeps the disc's own ink in both states — the disc
        // behind it already carries the selection, and the supplied icon set has
        // only one plus (KNOT-020k-fix5).
        if (tab.name === 'createStory') {
          return (
            <Pressable
              key={tab.name}
              style={styles.tab}
              onPress={() => onSelect(tab.name)}
              accessibilityRole="tab"
              accessibilityState={{ selected: active }}
              accessibilityLabel={label}
            >
              <View style={[styles.createButton, active ? styles.createButtonActive : null]}>
                <Icon
                  size={ICON_SIZE}
                  color={active ? colors.text.onBrand : colors.text.tertiary}
                  isActive={false}
                />
              </View>
            </Pressable>
          );
        }

        const tint = active ? colors.text.primary : colors.text.tertiary;

        return (
          <Pressable
            key={tab.name}
            style={styles.tab}
            onPress={() => onSelect(tab.name)}
            accessibilityRole="tab"
            accessibilityState={{ selected: active }}
            accessibilityLabel={label}
          >
            <View style={styles.glyph}>
              <TabGlyph icon={Icon} color={tint} active={active} />
              {unread > 0 ? (
                <View style={styles.badge} testID="tab-notifications-badge">
                  <Text style={[typeScale.caption, tabularNums, styles.badgeText]}>
                    {unread > 99 ? '99+' : String(unread)}
                  </Text>
                </View>
              ) : null}
            </View>
          </Pressable>
        );
      })}
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    badge: {
      alignItems: 'center',
      backgroundColor: colors.brand.primary,
      borderRadius: radius.pill,
      justifyContent: 'center',
      minWidth: 18,
      paddingHorizontal: spacing.xs,
      position: 'absolute',
      right: -spacing.md,
      top: -spacing.xs,
    },
    badgeText: {
      color: colors.text.onBrand,
    },
    bar: {
      alignItems: 'center',
      backgroundColor: colors.bg.canvas,
      borderTopColor: colors.border.hairline,
      borderTopWidth: 1,
      flexDirection: 'row',
      height: BAR_HEIGHT,
    },
    createButton: {
      alignItems: 'center',
      backgroundColor: colors.bg.sunken,
      borderRadius: radius.pill,
      height: CREATE_BUTTON_SIZE,
      justifyContent: 'center',
      width: CREATE_BUTTON_SIZE,
    },
    // The elevated action fills when it is the active tab. The disc carries the
    // selection, and the plus keeps the disc's own ink, so the pair reads as one
    // control rather than as a glyph drawn on top of a glyph (KNOT-020k-fix5).
    createButtonActive: {
      backgroundColor: colors.brand.primary,
    },
    // The glyph's own box, so the unread badge can hang off the bell's
    // top-right corner. There is no label beneath it any more, so the box is the
    // whole tab's content (KNOT-020k-fix).
    glyph: {
      alignItems: 'center',
      justifyContent: 'center',
      position: 'relative',
    },
    tab: {
      alignItems: 'center',
      flex: 1,
      justifyContent: 'center',
      minHeight: 44,
    },
  });

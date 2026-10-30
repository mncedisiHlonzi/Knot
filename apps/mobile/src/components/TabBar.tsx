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
import { BellIcon, ChatIcon, HomeIcon, MapIcon } from './TabIcons';
import type { TabIconProps } from './TabIcons';

/** The four primary destinations of the app. */
export type TabName = 'feed' | 'discoveryMap' | 'inquiries' | 'notifications';

/**
 * A tab glyph. Every one of the four is the founder's own SVG asset, rendered by
 * `TabIcons` rather than borrowed from an icon library (KNOT-020k-fix5): a tab's
 * state is a prop (`isActive`) rather than a second glyph component, because one
 * component owns both cuts of its own shape.
 */
type TabIcon = React.ComponentType<TabIconProps>;

/** One tab's definition: where it goes, its drawn name, and the glyph it draws. */
type TabDefinition = {
  readonly name: TabName;
  /** The name drawn under the glyph, and the name a screen reader speaks. */
  readonly label: string;
  /** The destination's SVG asset, drawn through `TabIcons`. */
  readonly icon: TabIcon;
};

/**
 * The tabs, in the order they appear left to right.
 *
 * Create is not one of them: it moved out of the bar to the composer row at the
 * top of the Feed, so the four remaining destinations share the bar evenly
 * (KNOT-020k-fix6).
 */
const TABS: readonly TabDefinition[] = [
  { name: 'feed', label: 'Home', icon: HomeIcon },
  { name: 'discoveryMap', label: 'Map', icon: MapIcon },
  // Inquiries has its own mark: a magnifier was the same glyph as the Feed
  // header's search control, which made two different destinations look like one
  // action (KNOT-020j-fix).
  { name: 'inquiries', label: 'Inquiries', icon: ChatIcon },
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
 * 24 points: a step down from the 28 the bar used while it was text free, so the
 * glyph and its label both fit the 64pt bar without crowding each other
 * (KNOT-020k-fix6). The artwork is on a 512-unit grid, so it scales cleanly.
 */
const ICON_SIZE = 24;

/**
 * How far the selected tab's glyph swells at the peak of its bounce, and how
 * much smaller it starts. Both are small enough to read as a settle rather than
 * as a bounce of its own (KNOT-020h).
 */
const BOUNCE_PEAK = 1.12;
const BOUNCE_START = 0.9;

/**
 * The smallest a label may shrink before it is truncated.
 *
 * "Notifications" is the longest name and the one that decides this: at four
 * equal columns the caption fits on most phones, and where it does not the label
 * gives up a fifth of its size rather than clipping (KNOT-020j).
 */
const LABEL_MIN_SCALE = 0.8;

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
 * The hand-rolled bottom tab bar: four destinations, no library.
 *
 * It is a plain row of buttons over the active screen. Non-tab screens (a story,
 * a question, a place, the composer) are pushed over the bar rather than shown
 * inside it, so the bar is only ever rendered for a primary destination. Using no
 * navigation dependency is deliberate; see KNOT-ADR-019.
 *
 * The four destinations are Home, Map, Inquiries, and Notifications. Inquiries is
 * a destination in its own right — finding and asking questions — rather than
 * something reached from Create (KNOT-ADR-058). Notifications took the slot after
 * Inquiries, and the bell left the Feed's header, so the inbox has a permanent,
 * always-reachable home; the profile it displaced is the Feed header's avatar.
 *
 * Each tab draws its name under its glyph again (KNOT-020k-fix6): the bar is a
 * quarter of the screen wide per destination, and a label is what makes a custom
 * icon set legible at a glance. The selected tab pairs the primary ink with the
 * solid (`*-fill`) cut of its asset, and every other tab pairs the tertiary ink
 * with the outline cut — colour and weight together, so the selection never rests
 * on hue alone. Creating a story is no longer a destination: the composer row at
 * the top of the Feed owns it, which leaves four evenly spaced destinations and
 * no elevated disc to crowd them (KNOT-020k-fix6).
 *
 * The bottom safe-area inset is not applied here: the bar already sits inside the
 * app root's `SafeAreaView` (KNOT-020b).
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
        const spoken =
          tab.name === 'notifications' && unread > 0
            ? `Notifications, ${unread} unread`
            : tab.label;
        const tint = active ? colors.text.primary : colors.text.tertiary;

        return (
          <Pressable
            key={tab.name}
            style={styles.tab}
            onPress={() => onSelect(tab.name)}
            accessibilityRole="tab"
            accessibilityState={{ selected: active }}
            accessibilityLabel={spoken}
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
            <Text
              style={[typeScale.caption, styles.label, active ? styles.labelActive : null]}
              numberOfLines={1}
              adjustsFontSizeToFit
              minimumFontScale={LABEL_MIN_SCALE}
            >
              {tab.label}
            </Text>
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
    // The glyph's own box, so the unread badge can hang off the bell's
    // top-right corner without stretching the column that holds the label.
    glyph: {
      alignItems: 'center',
      justifyContent: 'center',
      position: 'relative',
    },
    // The drawn name. It sits directly under the glyph, one 4pt step below it, in
    // the caption composite — the smallest of the app's type steps, which is what
    // a tab label should be — and takes the tab's ink with it.
    label: {
      color: colors.text.tertiary,
      marginTop: spacing.xs,
      textAlign: 'center',
    },
    labelActive: {
      color: colors.text.primary,
    },
    // One tab, one quarter of the row: the glyph and its name are centred as a
    // pair. The 44pt minimum keeps every tab a full touch target even though the
    // content is smaller than that now that the label is back.
    tab: {
      alignItems: 'center',
      flex: 1,
      justifyContent: 'center',
      minHeight: 44,
      paddingVertical: spacing.xs,
    },
  });

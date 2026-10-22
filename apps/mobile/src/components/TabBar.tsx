import React, { useEffect, useRef } from 'react';
import { Animated, Easing, Pressable, StyleSheet, Text, View } from 'react-native';
import { House, MagnifyingGlass, MapTrifold, Plus, User, type Icon } from 'phosphor-react-native';

import {
  fontWeights,
  motion,
  radius,
  spacing,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../theme';
import type { ThemeColors } from '../theme';

/** The five primary destinations of the app. */
export type TabName = 'feed' | 'discoveryMap' | 'createStory' | 'inquiries' | 'profile';

/** One tab's definition: where it goes, its label, and the icon it is drawn with. */
type TabDefinition = {
  readonly name: TabName;
  readonly label: string;
  readonly icon: Icon;
  /** The spoken label, when it differs from the visible one. */
  readonly accessibilityLabel?: string;
};

/** The tabs, in the order they appear left to right. */
const TABS: readonly TabDefinition[] = [
  { name: 'feed', label: 'Home', icon: House },
  { name: 'discoveryMap', label: 'Map', icon: MapTrifold },
  // The Create tab is the elevated centre button; its label is never drawn.
  { name: 'createStory', label: 'Create', icon: Plus, accessibilityLabel: 'Create story' },
  { name: 'inquiries', label: 'Inquiries', icon: MagnifyingGlass },
  { name: 'profile', label: 'Profile', icon: User },
];

type TabBarProps = {
  /** The tab currently shown. */
  readonly activeTab: TabName;
  /** Called when a tab is tapped. */
  readonly onSelect: (tab: TabName) => void;
};

/** The bar's height. The bottom safe-area inset is inherited from the app root. */
const BAR_HEIGHT = 56;

/** The icon size drawn in each tab. */
const ICON_SIZE = 24;

/** The diameter of the elevated Create button. */
const CREATE_BUTTON_SIZE = 44;

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
  icon: IconComponent,
  color,
  weight,
  active,
}: {
  readonly icon: Icon;
  readonly color: string;
  readonly weight: 'bold' | 'regular';
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
      <IconComponent color={color} size={ICON_SIZE} weight={weight} />
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
 * Inquiries joined as the fourth tab between Create and Profile (KNOT-ADR-058): it
 * is a destination in its own right — finding and asking questions — rather than
 * something reached from Create, which stays a story composer.
 *
 * Every tab is drawn with a Phosphor icon rather than a text glyph. The active tab
 * pairs the brand colour with the bold icon weight, so a selected tab reads
 * without colour alone. The Create tab is an elevated circular action in the brand
 * fill, matching the design reference. The bottom safe-area inset is not applied
 * here: the bar already sits inside the app root's `SafeAreaView` (KNOT-020b).
 */
export default function TabBar({ activeTab, onSelect }: TabBarProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  return (
    <View style={styles.bar} accessibilityRole="tablist">
      {TABS.map((tab) => {
        const active = tab.name === activeTab;
        const label = tab.accessibilityLabel ?? tab.label;

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
              <View style={styles.createButton}>
                <Plus color={colors.text.onBrand} size={ICON_SIZE} weight="bold" />
              </View>
            </Pressable>
          );
        }

        const IconComponent = tab.icon;
        const tint = active ? colors.brand.primary : colors.text.tertiary;

        return (
          <Pressable
            key={tab.name}
            style={styles.tab}
            onPress={() => onSelect(tab.name)}
            accessibilityRole="tab"
            accessibilityState={{ selected: active }}
            accessibilityLabel={label}
          >
            <TabGlyph
              icon={IconComponent}
              color={tint}
              weight={active ? 'bold' : 'regular'}
              active={active}
            />
            <Text
              style={[typeScale.caption, styles.label, active ? styles.labelActive : null]}
              numberOfLines={1}
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
      backgroundColor: colors.brand.primary,
      borderRadius: radius.pill,
      height: CREATE_BUTTON_SIZE,
      justifyContent: 'center',
      width: CREATE_BUTTON_SIZE,
    },
    label: {
      color: colors.text.tertiary,
      textAlign: 'center',
    },
    labelActive: {
      color: colors.brand.primary,
      fontWeight: fontWeights.semibold,
    },
    tab: {
      alignItems: 'center',
      flex: 1,
      gap: spacing.xs,
      justifyContent: 'center',
      minHeight: 44,
    },
  });

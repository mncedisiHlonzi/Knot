import React from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { colors, fontSizes, fontWeights, spacing } from '../theme';

/** The four primary destinations of the app. */
export type TabName = 'feed' | 'discoveryMap' | 'createStory' | 'profile';

/** The tabs, in the order they appear left to right. */
const TABS: readonly { readonly name: TabName; readonly label: string }[] = [
  { name: 'feed', label: 'Home' },
  { name: 'discoveryMap', label: 'Map' },
  { name: 'createStory', label: 'Create' },
  { name: 'profile', label: 'Profile' },
];

type TabBarProps = {
  /** The tab currently shown. */
  readonly activeTab: TabName;
  /** Called when a tab is tapped. */
  readonly onSelect: (tab: TabName) => void;
};

/**
 * The hand-rolled bottom tab bar: four destinations, no library.
 *
 * It is a plain row of buttons over the active screen. Non-tab screens (a story,
 * a place, the composer's overlays) are pushed over the bar rather than shown
 * inside it, so the bar is only ever rendered for a primary destination. Using no
 * navigation dependency is deliberate; see KNOT-ADR-019.
 */
export default function TabBar({ activeTab, onSelect }: TabBarProps): React.ReactElement {
  return (
    <View style={styles.bar} accessibilityRole="tablist">
      {TABS.map((tab) => {
        const active = tab.name === activeTab;
        return (
          <Pressable
            key={tab.name}
            style={styles.tab}
            onPress={() => onSelect(tab.name)}
            accessibilityRole="tab"
            accessibilityState={{ selected: active }}
            accessibilityLabel={tab.label}
          >
            <Text style={[styles.label, active ? styles.labelActive : null]}>{tab.label}</Text>
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  bar: {
    backgroundColor: colors.bg.inverse,
    borderTopColor: colors.border.default,
    borderTopWidth: 1,
    flexDirection: 'row',
    paddingVertical: spacing.sm,
  },
  label: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.medium,
  },
  labelActive: {
    color: colors.text.brand,
    fontWeight: fontWeights.semiBold,
  },
  tab: {
    alignItems: 'center',
    flex: 1,
    paddingVertical: spacing.sm,
  },
});

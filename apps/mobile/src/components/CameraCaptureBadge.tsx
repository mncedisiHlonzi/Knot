import React from 'react';
import { StyleProp, StyleSheet, Text, View, ViewStyle } from 'react-native';

import { colors, fontSizes, fontWeights, radius, spacing } from '../theme';

type CameraCaptureBadgeProps = {
  /** Optional short label shown next to the icon. */
  readonly label?: string;
  /** Extra positioning, so a parent can pin the badge to a thumbnail corner. */
  readonly style?: StyleProp<ViewStyle>;
};

/**
 * A small badge marking media that was captured with the device camera rather
 * than chosen from the gallery.
 *
 * It draws its icon as a unicode character so it needs no new dependency and no
 * asset. The parent positions it (typically in a corner of a thumbnail) by
 * passing `style`; otherwise it renders inline as a pill.
 */
export default function CameraCaptureBadge({
  label,
  style,
}: CameraCaptureBadgeProps): React.ReactElement {
  return (
    <View style={[styles.badge, style]} accessibilityLabel="Captured with the camera">
      <Text style={styles.icon}>📷</Text>
      {label !== undefined ? <Text style={styles.label}>{label}</Text> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  badge: {
    alignItems: 'center',
    backgroundColor: colors.bg.secondary,
    borderColor: colors.border.subtle,
    borderRadius: radius.pill,
    borderWidth: 1,
    flexDirection: 'row',
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
  },
  icon: {
    fontSize: fontSizes.sm,
  },
  label: {
    color: colors.text.primary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.medium,
    marginLeft: spacing.xs,
  },
});

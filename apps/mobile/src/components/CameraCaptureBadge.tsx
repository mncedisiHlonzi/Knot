import React from 'react';
import { StyleProp, StyleSheet, Text, View, ViewStyle } from 'react-native';

import { radius, spacing, typeScale, useThemedStyles, type ThemeColors } from '../theme';

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
  const styles = useThemedStyles(createStyles);
  return (
    <View style={[styles.badge, style]} accessibilityLabel="Captured with the camera">
      {/* The camera glyph is an emoji, so its ink is the glyph's own; only the
          type scale applies (KNOT-020g-fix15). */}
      <Text style={typeScale.bodyS}>📷</Text>
      {label !== undefined ? <Text style={[typeScale.caption, styles.label]}>{label}</Text> : null}
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    badge: {
      alignItems: 'center',
      backgroundColor: colors.bg.sunken,
      borderColor: colors.border.hairline,
      borderRadius: radius.pill,
      borderWidth: 1,
      flexDirection: 'row',
      paddingHorizontal: spacing.sm,
      paddingVertical: 2,
    },
    label: {
      color: colors.text.primary,
      marginLeft: spacing.xs,
    },
  });

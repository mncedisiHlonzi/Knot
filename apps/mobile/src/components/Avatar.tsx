import React from 'react';
import { Image, Pressable, StyleSheet, Text, View } from 'react-native';

import { fontWeights, radius, spacing, useThemedStyles, type ThemeColors } from '../theme';
import { getInitials } from '../utils/initials';

/** The five avatar diameters the design system offers. */
export type AvatarSize = 'xs' | 'sm' | 'md' | 'lg' | 'xl';

type AvatarProps = {
  /** A full image URI, or null/empty to fall back to initials. */
  readonly uri?: string | null;
  /** The person's name, used for the initials fallback and the label. */
  readonly displayName: string;
  /** The diameter. Defaults to `md` (32). */
  readonly size?: AvatarSize;
  /** When given, the avatar becomes a tap target (with a 44-point hit area). */
  readonly onPress?: () => void;
  /** Overrides the accessibility label. */
  readonly accessibilityLabel?: string;
  /** Test identifier. */
  readonly testID?: string;
};

/** Avatar diameter per size, in points. */
const DIAMETER: Readonly<Record<AvatarSize, number>> = {
  xs: 20,
  sm: 24,
  md: 32,
  lg: 48,
  xl: 88,
};

/**
 * Initials size per size, in points. There is no type token that scales with the
 * diameter, so the size is proportional to it (about 0.45 of the diameter, the
 * ratio the design reference uses for its 32/40/88 avatars).
 */
const INITIALS_SIZE: Readonly<Record<AvatarSize, number>> = {
  xs: 10,
  sm: 11,
  md: 15,
  lg: 20,
  xl: 40,
};

/** Expands a small avatar (xs/sm) to a comfortable touch target when tappable. */
const HIT_SLOP = spacing.md;

/**
 * A person's avatar: their photo when one is set, their initials otherwise.
 *
 * The two forms are mutually exclusive — an image URI that is present but blank
 * falls back to initials rather than rendering an empty frame. The initials
 * always resolve (via `getInitials`, which yields "?" for a nameless person), so
 * there is always something to draw.
 */
export default function Avatar({
  uri,
  displayName,
  size = 'md',
  onPress,
  accessibilityLabel,
  testID,
}: AvatarProps): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  const diameter = DIAMETER[size];
  const path = typeof uri === 'string' ? uri.trim() : '';
  const hasImage = path !== '';

  const name = typeof displayName === 'string' ? displayName.trim() : '';
  const label = accessibilityLabel ?? (name === '' ? 'Avatar' : `${name}'s avatar`);

  const content = hasImage ? (
    <Image
      source={{ uri: path }}
      style={[styles.image, { height: diameter, width: diameter }]}
      accessibilityIgnoresInvertColors
    />
  ) : (
    <View style={[styles.fallback, { height: diameter, width: diameter }]}>
      <Text style={[styles.initials, { fontSize: INITIALS_SIZE[size] }]}>
        {getInitials(displayName)}
      </Text>
    </View>
  );

  if (onPress === undefined) {
    return (
      <View style={styles.wrapper} accessibilityLabel={label} testID={testID}>
        {content}
      </View>
    );
  }

  return (
    <Pressable
      onPress={onPress}
      hitSlop={HIT_SLOP}
      style={styles.wrapper}
      accessibilityRole="button"
      accessibilityLabel={label}
      testID={testID}
    >
      {content}
    </Pressable>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    fallback: {
      alignItems: 'center',
      backgroundColor: colors.bg.sunken,
      borderRadius: radius.pill,
      justifyContent: 'center',
    },
    image: {
      backgroundColor: colors.bg.sunken,
      borderRadius: radius.pill,
    },
    initials: {
      color: colors.text.secondary,
      fontWeight: fontWeights.semibold,
    },
    wrapper: {
      alignSelf: 'flex-start',
    },
  });

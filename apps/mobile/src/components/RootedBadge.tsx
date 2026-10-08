import React from 'react';
import { StyleSheet, Text } from 'react-native';

import { colors, fontSizes, fontWeights, radius, spacing } from '../theme';

/** Display labels for the duration buckets, keyed by the server's values. */
const DURATION_LABELS: Readonly<Record<string, string>> = {
  lifelong: 'Lifelong',
  many_years: 'Many years',
  several_years: 'Several years',
  a_few_years: 'A few years',
  recently: 'Recently',
};

type RootedBadgeProps = {
  /** The declared place, at city or region precision. */
  readonly place: string;
  /** The declared duration bucket. Optional: the badge still shows the place. */
  readonly durationBucket?: string | null;
  /** Renders a smaller badge for dense rows such as a comment's meta line. */
  readonly compact?: boolean;
};

/**
 * RootedBadge is the inline Rooted summary: a small pill showing where a person
 * is Rooted, plus a short duration label.
 *
 * It renders nothing when there is no place to show, so a caller can render it
 * unconditionally for an author whose Rooted summary may be absent. This is a
 * summary, not a lookup: it takes plain strings rather than a signal object, so
 * it never needs the id, the owner, or the timestamps.
 */
export default function RootedBadge({
  place,
  durationBucket,
  compact,
}: RootedBadgeProps): React.ReactElement | null {
  const trimmedPlace = place.trim();
  if (trimmedPlace === '') {
    return null;
  }

  const label =
    durationBucket === undefined || durationBucket === null
      ? undefined
      : DURATION_LABELS[durationBucket];
  const text = label === undefined ? trimmedPlace : `${trimmedPlace} · ${label}`;

  return (
    <Text
      style={[styles.badge, compact === true ? styles.compact : null]}
      numberOfLines={1}
      accessibilityLabel={
        label === undefined ? `Rooted in ${trimmedPlace}` : `Rooted in ${trimmedPlace}, ${label}`
      }
    >
      {text}
    </Text>
  );
}

const styles = StyleSheet.create({
  badge: {
    alignSelf: 'flex-start',
    backgroundColor: colors.border.subtle,
    borderRadius: radius.pill,
    color: colors.text.primary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.medium,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
  },
  compact: {
    fontSize: fontSizes.xs,
  },
});

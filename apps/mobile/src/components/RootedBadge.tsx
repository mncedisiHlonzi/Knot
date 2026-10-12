import React from 'react';
import { StyleSheet, Text, View } from 'react-native';

import { colors, radius, spacing, typeScale } from '../theme';

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
  /** The declared duration bucket, as the server stores it. */
  readonly durationBucket: string;
  /** Compact form: "Rooted · place" without the duration. */
  readonly compact?: boolean;
  /** Test identifier. */
  readonly testID?: string;
};

/**
 * RootedBadge is the inline Rooted summary: a small pill showing where a person
 * is Rooted, plus a short duration label.
 *
 * It is the one place the Rooted attribution is drawn, on a brand surface fill in
 * the brand ink. It renders nothing when there is no place to show, so a caller
 * can render it unconditionally for an author whose Rooted summary may be absent.
 * This is a summary, not a lookup: it takes plain strings rather than a signal
 * object, so it never needs the id, the owner, or the timestamps.
 *
 * The place is the segment allowed to shrink, so a long place name truncates
 * before the duration label does.
 */
export default function RootedBadge({
  place,
  durationBucket,
  compact = false,
  testID,
}: RootedBadgeProps): React.ReactElement | null {
  // Guarded with typeof: the API response types describe an optimistic wire
  // format, so a value may be absent or a non-string at runtime (KNOT-015b-fix).
  const trimmedPlace = typeof place === 'string' ? place.trim() : '';
  if (trimmedPlace === '') {
    return null;
  }

  const bucket = typeof durationBucket === 'string' ? durationBucket : '';
  const label = bucket === '' ? undefined : DURATION_LABELS[bucket];
  const showDuration = compact !== true && label !== undefined;

  return (
    <View
      style={[styles.badge, compact ? styles.compact : null]}
      testID={testID}
      accessibilityLabel={
        label === undefined ? `Rooted in ${trimmedPlace}` : `Rooted in ${trimmedPlace}, ${label}`
      }
    >
      <Text style={[typeScale.caption, styles.text]} numberOfLines={1}>
        Rooted ·{' '}
      </Text>
      <Text style={[typeScale.caption, styles.text, styles.place]} numberOfLines={1}>
        {trimmedPlace}
      </Text>
      {showDuration && label !== undefined ? (
        <Text style={[typeScale.caption, styles.text]} numberOfLines={1}>
          {` · ${label}`}
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  badge: {
    alignItems: 'center',
    alignSelf: 'flex-start',
    backgroundColor: colors.brand.surface,
    borderRadius: radius.sm,
    flexDirection: 'row',
    maxWidth: '100%',
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
  },
  compact: {
    paddingHorizontal: spacing.sm,
  },
  place: {
    flexShrink: 1,
  },
  text: {
    color: colors.brand.primary,
  },
});

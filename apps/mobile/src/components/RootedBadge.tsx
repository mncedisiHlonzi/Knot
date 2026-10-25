import React from 'react';
import { StyleSheet, Text, View } from 'react-native';

import {
  fontWeights,
  radius,
  spacing,
  typeScale,
  useThemedStyles,
  type ThemeColors,
} from '../theme';

/** Display labels for the duration buckets, keyed by the server's values. */
const DURATION_LABELS: Readonly<Record<string, string>> = {
  lifelong: 'Lifelong',
  many_years: 'Many years',
  several_years: 'Several years',
  a_few_years: 'A few years',
  recently: 'Recently',
};

/** The two presentations a Rooted summary can take. */
export type RootedBadgeVariant = 'badge' | 'quiet';

type RootedBadgeProps = {
  /** The declared place, at city or region precision. */
  readonly place: string;
  /** The declared duration bucket, as the server stores it. */
  readonly durationBucket: string;
  /** Compact form: "Rooted · place" without the duration. */
  readonly compact?: boolean;
  /**
   * Whether the duration label is drawn. Defaults to `true`, the full form every
   * caller but the Feed wants. The Feed passes `false`, so its quiet line reads
   * `Rooted · {place}` (KNOT-020c-fix5).
   */
  readonly showDuration?: boolean;
  /**
   * `badge` (the default) is the brand pill, for an inline attribution beside a
   * name. `quiet` is plain tertiary text with no fill, for a second line under a
   * name where the attribution is a detail rather than a highlight
   * (KNOT-020c-fix2).
   */
  readonly variant?: RootedBadgeVariant;
  /** Test identifier. */
  readonly testID?: string;
};

/**
 * RootedBadge is the inline Rooted summary: a small pill showing where a person
 * is Rooted, plus a short duration label. It is text only — the Rooted mark that
 * used to lead the pill was removed so the label itself carries the emphasis, set
 * in the bold weight on the brand surface (KNOT-020k-fix24).
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
  showDuration = true,
  variant = 'badge',
  testID,
}: RootedBadgeProps): React.ReactElement | null {
  const styles = useThemedStyles(createStyles);
  // Guarded with typeof: the API response types describe an optimistic wire
  // format, so a value may be absent or a non-string at runtime (KNOT-015b-fix).
  const trimmedPlace = typeof place === 'string' ? place.trim() : '';
  if (trimmedPlace === '') {
    return null;
  }

  const bucket = typeof durationBucket === 'string' ? durationBucket : '';
  const label = bucket === '' ? undefined : DURATION_LABELS[bucket];
  // The duration is drawn only when the caller wants it and the bucket is known.
  const durationLabel = showDuration && label !== undefined ? label : undefined;

  // The accessible label follows what is actually drawn, so a shortened line is
  // not announced with a segment it does not show.
  const accessibleLabel =
    label === undefined ? `Rooted in ${trimmedPlace}` : `Rooted in ${trimmedPlace}, ${label}`;

  // The quiet presentation is a line of its own: plain tertiary text with no
  // surface fill and no border, exactly as the design reference draws it
  // (KNOT-020c-fix3). The Feed asks for the place alone, so it reads
  // "Rooted · {place}" (KNOT-020c-fix5). It takes the same bold weight as the
  // pill so the attribution reads the same way wherever it is drawn
  // (KNOT-020k-fix24).
  if (variant === 'quiet') {
    const spoken =
      durationLabel === undefined
        ? `Rooted in ${trimmedPlace}`
        : `Rooted in ${trimmedPlace}, ${durationLabel}`;

    return (
      <View style={styles.quiet} testID={testID} accessibilityLabel={spoken}>
        <Text style={[typeScale.caption, styles.quietText]} numberOfLines={1}>
          {durationLabel === undefined
            ? `Rooted · ${trimmedPlace}`
            : `Rooted · ${trimmedPlace} · ${durationLabel}`}
        </Text>
      </View>
    );
  }

  // The compact badge drops the duration too; otherwise it follows the caller.
  const badgeDuration = compact ? undefined : durationLabel;

  return (
    <View
      style={[styles.badge, compact ? styles.compact : null]}
      testID={testID}
      accessibilityLabel={accessibleLabel}
    >
      <View style={styles.textRow}>
        <Text style={[typeScale.caption, styles.text]} numberOfLines={1}>
          Rooted ·{' '}
        </Text>
        <Text style={[typeScale.caption, styles.text, styles.place]} numberOfLines={1}>
          {trimmedPlace}
        </Text>
        {badgeDuration === undefined ? null : (
          <Text style={[typeScale.caption, styles.text]} numberOfLines={1}>
            {` · ${badgeDuration}`}
          </Text>
        )}
      </View>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
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
    quiet: {
      alignSelf: 'flex-start',
      maxWidth: '100%',
    },
    quietText: {
      color: colors.text.tertiary,
      flexShrink: 1,
      fontWeight: fontWeights.bold,
    },
    text: {
      color: colors.brand.primary,
      fontWeight: fontWeights.bold,
    },
    textRow: {
      alignItems: 'center',
      flexDirection: 'row',
      flexShrink: 1,
    },
  });

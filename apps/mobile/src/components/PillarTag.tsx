import React from 'react';
import { StyleSheet, Text, View } from 'react-native';

import { radius, spacing, typeScale, useTheme, useThemedStyles } from '../theme';
import type { ThemeColors } from '../theme';

/** The two pillars every story is told under. */
export type Pillar = 'wonder' | 'heritage';

/** The two presentations a pillar tag can take. */
export type PillarTagVariant = 'filled' | 'plain';

type PillarTagProps = {
  /** Which pillar the content belongs to. */
  readonly pillar: Pillar;
  /** An optional place, appended as " · PLACE" in the tertiary ink. */
  readonly place?: string | null;
  /**
   * `filled` (the default) is the pill on the pillar's own surface tint — the
   * form the Story Detail and every other screen use today. `plain` drops the
   * fill, the radius and the horizontal padding, leaving upper-cased pillar ink on
   * the canvas, flush with the rest of the Feed entry, which is what the feed's
   * design reference asks for (KNOT-020c-fix3, KNOT-020c-fix6).
   */
  readonly variant?: PillarTagVariant;
  /** Test identifier. */
  readonly testID?: string;
};

/** The word shown for each pillar. */
const PILLAR_LABELS: Readonly<Record<Pillar, string>> = {
  wonder: 'Wonder',
  heritage: 'Heritage',
};

/**
 * The pillar tag: the label naming a story's pillar and, optionally, its place.
 *
 * Wonder and Heritage each carry their own colour pair from the token layer, so
 * this is the one place the pillar palette is applied. The label is upper-cased by
 * the `overline` type style; the place is drawn in the quieter tertiary ink so the
 * pillar word stays the emphasis.
 *
 * The `filled` variant is a small pill on the pillar's surface tint. The `plain`
 * variant is the same text with no surface at all — no fill, no border, no radius
 * and no horizontal padding — so the feed reads as text on the canvas rather than
 * as a chip, and its left edge lines up with the avatar and the story text
 * (KNOT-020c-fix6).
 */
export default function PillarTag({
  pillar,
  place,
  variant = 'filled',
  testID,
}: PillarTagProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const pillarColors = colors.pillar[pillar];
  const placeText = typeof place === 'string' ? place.trim() : '';

  const label = placeText === '' ? PILLAR_LABELS[pillar] : `${PILLAR_LABELS[pillar]}, ${placeText}`;

  return (
    <View
      style={[
        styles.tag,
        variant === 'plain' ? styles.plain : { backgroundColor: pillarColors.fill },
      ]}
      testID={testID}
      accessibilityLabel={label}
    >
      <Text style={[typeScale.overline, { color: pillarColors.text }]} numberOfLines={1}>
        {PILLAR_LABELS[pillar]}
        {placeText !== '' ? <Text style={styles.place}>{` · ${placeText}`}</Text> : null}
      </Text>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    place: {
      color: colors.text.tertiary,
    },
    // No fill, no border, no radius and no horizontal padding: the tag becomes
    // plain text on the canvas. The horizontal padding must be reset here because
    // the base style carries the pill's `paddingHorizontal`; without this the tag
    // starts a step in from the rest of the entry (KNOT-020c-fix6).
    plain: {
      alignSelf: 'flex-start',
      paddingHorizontal: 0,
    },
    tag: {
      alignSelf: 'flex-start',
      borderRadius: radius.sm,
      paddingHorizontal: spacing.sm,
      paddingVertical: spacing.xs,
    },
  });

import React from 'react';
import { StyleSheet, Text, View } from 'react-native';

import { colors, radius, spacing, typeScale } from '../theme';

/** The two pillars every story is told under. */
export type Pillar = 'wonder' | 'heritage';

type PillarTagProps = {
  /** Which pillar the content belongs to. */
  readonly pillar: Pillar;
  /** An optional place, appended as " · PLACE" in the tertiary ink. */
  readonly place?: string | null;
  /** Test identifier. */
  readonly testID?: string;
};

/** The word shown for each pillar. */
const PILLAR_LABELS: Readonly<Record<Pillar, string>> = {
  wonder: 'Wonder',
  heritage: 'Heritage',
};

/**
 * The pillar tag: a small filled tag naming a story's pillar and, optionally, its
 * place.
 *
 * Wonder and Heritage each carry their own colour pair from the token layer, so
 * the tag is the one place the pillar palette is applied. The label is upper-cased
 * by the `overline` type style; the place is drawn in the quieter tertiary ink so
 * the pillar word stays the emphasis.
 */
export default function PillarTag({ pillar, place, testID }: PillarTagProps): React.ReactElement {
  const pillarColors = colors.pillar[pillar];
  const placeText = typeof place === 'string' ? place.trim() : '';

  return (
    <View
      style={[styles.tag, { backgroundColor: pillarColors.fill }]}
      testID={testID}
      accessibilityLabel={
        placeText === '' ? PILLAR_LABELS[pillar] : `${PILLAR_LABELS[pillar]}, ${placeText}`
      }
    >
      <Text style={[typeScale.overline, { color: pillarColors.text }]} numberOfLines={1}>
        {PILLAR_LABELS[pillar]}
        {placeText !== '' ? <Text style={styles.place}>{` · ${placeText}`}</Text> : null}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  place: {
    color: colors.text.tertiary,
  },
  tag: {
    alignSelf: 'flex-start',
    borderRadius: radius.sm,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
  },
});

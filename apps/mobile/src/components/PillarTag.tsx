import React from 'react';
import { StyleSheet, Text } from 'react-native';

import { fontWeights, typeScale, useThemedStyles } from '../theme';
import type { ThemeColors } from '../theme';

/** The two pillars every story is told under. */
export type Pillar = 'wonder' | 'heritage';

type PillarTagProps = {
  /** Which pillar the content belongs to. */
  readonly pillar: Pillar;
  /** An optional place, appended as " · {place}". */
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
 * The pillar line: the label naming a story's pillar and, optionally, its place.
 *
 * It is **bold text and nothing else** (KNOT-020k, KNOT-ADR-096): no overline, no
 * underline, no letter-spacing, no mark, no pill, no background and no border. The
 * line reads `Wonder · Manguzi` in normal case, in the mode's primary ink, so a
 * pillar frames a story the way a byline does rather than as a tinted category
 * chip. The two pillars no longer carry colours of their own: the category palette
 * was retired with the indigo design reference.
 *
 * A caller with no place renders the pillar alone.
 */
export default function PillarTag({ pillar, place, testID }: PillarTagProps): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  const placeText = typeof place === 'string' ? place.trim() : '';
  const label =
    placeText === '' ? PILLAR_LABELS[pillar] : `${PILLAR_LABELS[pillar]} · ${placeText}`;

  return (
    <Text style={styles.line} numberOfLines={1} testID={testID} accessibilityLabel={label}>
      {label}
    </Text>
  );
}

/**
 * The line's style.
 *
 * `caption` is the nearest composite to the removed overline's size, at the bold
 * weight the founder asked for, in the mode's primary ink. There is deliberately
 * no `textTransform`, no `textDecorationLine` and no `letterSpacing`: the pillar is
 * a sentence fragment, not a label.
 */
const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    line: {
      ...typeScale.caption,
      color: colors.text.primary,
      fontWeight: fontWeights.bold,
      // The `caption` composite carries the design's tracking, which belongs to a
      // label rather than to this line; the pillar reads as prose (KNOT-020k).
      letterSpacing: 0,
    },
  });

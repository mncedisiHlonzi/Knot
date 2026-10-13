import React from 'react';
import { StyleSheet, Text, View } from 'react-native';

import { colors, spacing, typeScale } from '../theme';

type HeaderProps = {
  /** A centred title. When omitted, the middle stays empty. */
  readonly title?: string;
  /** A leading node — typically a 44-point icon button. */
  readonly left?: React.ReactNode;
  /** A trailing node, or a row of them. */
  readonly right?: React.ReactNode;
  /**
   * Pins the header: it paints a constant opaque canvas fill, so scrolled content
   * can never show through behind its icons. Defaults to `false`, which leaves the
   * header transparent so it can float over content — the Story Detail hero is
   * the case that needs that.
   */
  readonly pinned?: boolean;
  /** Test identifier. */
  readonly testID?: string;
};

/** The header's height below the status bar. */
const HEIGHT = 56;

/**
 * The screen header: a 56-point bar with a centred title between two slots.
 *
 * It is deliberately **static**: nothing about it is tied to the scroll offset —
 * it never fades, shifts, or casts a scroll-linked shadow — so it always reads at
 * full strength (KNOT-020c-fix4). A **pinned** header (the Feed's) paints a
 * constant opaque canvas fill so content scrolls under it cleanly; an unpinned
 * header (the Story Detail's) paints nothing, so it floats transparently over
 * hero content.
 *
 * The status-bar inset is not applied here: every screen already sits inside the
 * app root's `SafeAreaView`, so adding another inset would double it (KNOT-020b).
 */
export default function Header({
  title,
  left,
  right,
  pinned = false,
  testID,
}: HeaderProps): React.ReactElement {
  return (
    <View style={styles.container} testID={testID}>
      {/* A constant opaque fill: the bar never fades, so no scroll offset is
          needed here at all. */}
      {pinned ? <View pointerEvents="none" style={styles.background} /> : null}
      <View style={styles.row}>
        <View style={styles.side}>{left}</View>
        {title !== undefined ? (
          <Text style={[typeScale.h3, styles.title]} numberOfLines={1}>
            {title}
          </Text>
        ) : (
          <View style={styles.titleSpacer} />
        )}
        <View style={styles.side}>{right}</View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  background: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: colors.bg.canvas,
  },
  container: {
    height: HEIGHT,
    justifyContent: 'center',
  },
  row: {
    alignItems: 'center',
    flexDirection: 'row',
    height: HEIGHT,
    justifyContent: 'space-between',
    paddingHorizontal: spacing.sm,
  },
  side: {
    alignItems: 'center',
    flexDirection: 'row',
  },
  title: {
    color: colors.text.primary,
    flex: 1,
    textAlign: 'center',
  },
  titleSpacer: {
    flex: 1,
  },
});

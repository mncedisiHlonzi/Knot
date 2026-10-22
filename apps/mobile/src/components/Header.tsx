import React from 'react';
import { StyleSheet, Text, View } from 'react-native';

import { spacing, typeScale, useThemedStyles, type ThemeColors } from '../theme';

type HeaderProps = {
  /** The title, always centred on the bar. When omitted, the middle stays empty. */
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
 * The inset the title keeps from each edge, so a long one cannot run under a side
 * slot's control.
 */
const TITLE_INSET = 56;

/**
 * The screen header: a 56-point bar with a title centred on the bar itself.
 *
 * It is deliberately **static**: nothing about it is tied to the scroll offset —
 * it never fades, shifts, or casts a scroll-linked shadow — so it always reads at
 * full strength (KNOT-020c-fix4). A **pinned** header (the Feed's) paints a
 * constant opaque canvas fill so content scrolls under it cleanly; an unpinned
 * header (the Story Detail's) paints nothing, so it floats transparently over
 * hero content.
 *
 * The title is drawn **out of flow**, centred on the bar rather than on the space
 * left between the two side slots. That is what keeps it on the screen's centre
 * when the two slots differ in width — a back control on one side and nothing on
 * the other, say — which the in-flow form could not do. The side slots stay in
 * their own row and keep their own corners (KNOT-020g-fix4, KNOT-020g-fix13).
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
  const styles = useThemedStyles(createStyles);
  return (
    <View style={styles.container} testID={testID}>
      {/* A constant opaque fill: the bar never fades, so no scroll offset is
          needed here at all. */}
      {pinned ? <View pointerEvents="none" style={styles.background} /> : null}
      <View style={styles.row}>
        <View style={styles.side}>{left}</View>
        {/* The title is drawn out of flow (below), so the middle stays a flexible
            spacer and the two slots take their own corners. */}
        <View style={styles.titleSpacer} />
        <View style={styles.side}>{right}</View>

        {title !== undefined ? (
          // Out of flow, so an unequal pair of side slots cannot pull it off the
          // bar's centre. The inset keeps a long title clear of those slots.
          <View pointerEvents="none" style={styles.titleCentered}>
            <Text style={[typeScale.h3, styles.title]} numberOfLines={1}>
              {title}
            </Text>
          </View>
        ) : null}
      </View>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
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
    // Pinned to the bar's own middle, so an unequal pair of side slots cannot pull
    // the title off centre (KNOT-020g-fix4, KNOT-020g-fix13). The inset keeps a long
    // title clear of the side slots; a short one is genuinely centred.
    titleCentered: {
      left: 0,
      paddingHorizontal: TITLE_INSET,
      position: 'absolute',
      right: 0,
    },
    titleSpacer: {
      flex: 1,
    },
  });

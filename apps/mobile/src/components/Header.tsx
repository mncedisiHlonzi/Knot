import React, { useEffect, useState } from 'react';
import { Animated, StyleSheet, Text, View } from 'react-native';

import { colors, elevation, spacing, typeScale } from '../theme';

type HeaderProps = {
  /** A centred title. When omitted, the middle stays empty. */
  readonly title?: string;
  /** A leading node — typically a 44-point icon button. */
  readonly left?: React.ReactNode;
  /** A trailing node, or a row of them. */
  readonly right?: React.ReactNode;
  /**
   * The scroll offset of the list beneath the header. When given, a first-level
   * shadow appears once the content has scrolled past a small threshold, so a
   * scrolling list reads as passing under a fixed bar.
   */
  readonly scrollY?: Animated.Value;
  /**
   * Pins the header: it paints an opaque canvas fill at all times and never fades,
   * translates, or disappears as the list scrolls (KNOT-020c-fix2). Defaults to
   * `false`, which leaves the header transparent so it can float over content —
   * the Story Detail hero is the case that needs that.
   */
  readonly pinned?: boolean;
  /** Test identifier. */
  readonly testID?: string;
};

/** The header's height below the status bar. */
const HEIGHT = 56;

/** The scroll offset past which the header lifts off the canvas. */
const ELEVATION_THRESHOLD = 8;

/**
 * The screen header: a 56-point bar with a centred title between two slots.
 *
 * A **pinned** header (the Feed's) paints an opaque canvas fill and holds still:
 * nothing about it changes unless the caller asks for scroll-linked elevation, in
 * which case only the first-level shadow appears, past an 8-point threshold. It
 * never fades or translates (KNOT-020c-fix2). An unpinned header (the Story
 * Detail's) paints nothing, so it floats transparently over hero content.
 *
 * The status-bar inset is not applied here: every screen already sits inside the
 * app root's `SafeAreaView`, so adding another inset would double it (KNOT-020b).
 */
export default function Header({
  title,
  left,
  right,
  scrollY,
  pinned = false,
  testID,
}: HeaderProps): React.ReactElement {
  const [elevated, setElevated] = useState(false);

  useEffect(() => {
    if (scrollY === undefined) {
      return undefined;
    }
    const id = scrollY.addListener(({ value }) => {
      setElevated(value > ELEVATION_THRESHOLD);
    });
    return () => {
      scrollY.removeListener(id);
    };
  }, [scrollY]);

  return (
    <View style={styles.container} testID={testID}>
      {/* The only scroll-linked change is the shadow lifting past the threshold;
          the fill itself is constant, so the bar never fades. */}
      {pinned ? (
        <View pointerEvents="none" style={[styles.background, elevated ? elevation[1] : null]} />
      ) : null}
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

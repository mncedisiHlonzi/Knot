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
   * The scroll offset of the list beneath the header. When given, the header's
   * canvas background fades in as the content scrolls under it and a hairline
   * shadow appears past a small threshold.
   */
  readonly scrollY?: Animated.Value;
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
 * It is transparent at rest so the canvas shows through, and fades in the canvas
 * fill (plus a first-level shadow) as content scrolls beneath it, so a scrolling
 * list never collides with the title. The status-bar inset is not applied here:
 * every screen already sits inside the app root's `SafeAreaView`, so adding
 * another inset would double it (KNOT-020b).
 */
export default function Header({
  title,
  left,
  right,
  scrollY,
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

  // With no scroll offset the bar stays transparent; with one, it fades to the
  // canvas colour over the first screenful of scroll.
  const backgroundOpacity =
    scrollY === undefined
      ? 0
      : scrollY.interpolate({
          inputRange: [0, HEIGHT],
          outputRange: [0, 1],
          extrapolate: 'clamp',
        });

  return (
    <View style={styles.container} testID={testID}>
      <Animated.View
        pointerEvents="none"
        style={[
          styles.background,
          elevated ? elevation[1] : null,
          { backgroundColor: colors.bg.canvas, opacity: backgroundOpacity },
        ]}
      />
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

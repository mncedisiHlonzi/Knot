import React, { useEffect, useRef } from 'react';
import { Animated, Easing, StyleSheet, View } from 'react-native';

import { motion, useThemedStyles, type ThemeColors } from '../theme';

/** The line's thickness, per the design direction. */
const THICKNESS = 1.5;

/** The drawn line's default extent, in points. */
const DEFAULT_LENGTH = 120;

/**
 * Where the line has faded out by, as a fraction of the draw. The fade is what
 * makes `Animated.loop`'s reset invisible: without it the line would jump back to
 * zero length at the end of every cycle.
 */
const FADE_START = 0.82;

type TheThreadProps = {
  /**
   * Which way the line draws: across a surface (`horizontal`, the default) or
   * down it (`vertical`, the tree's skeleton).
   */
  readonly orientation?: 'horizontal' | 'vertical';
  /** The line's full extent, in points. */
  readonly length?: number;
  /** Test identifier. */
  readonly testID?: string;
};

/**
 * The Thread: the signature 1.5-point indigo line that draws itself.
 *
 * It is the app's one loading gesture (KNOT-020h). The line grows from nothing to
 * its full extent on a loop, using the motion system's `standard` curve and its
 * longest duration, then fades out briefly so the loop restarts without a jump.
 *
 * It is drawn with `Animated` on the native driver: the grow is a `scaleX` /
 * `scaleY` transform offset by half the length, which keeps the leading edge at
 * the start of the line (a bare `scale` would grow from the middle). `opacity` is
 * interpolated from the same value, so one timing drives both and nothing touches
 * the JS thread per frame.
 *
 * The frame is a fixed `length` × `THICKNESS` box so the surrounding layout never
 * moves while the line draws — the buttons, the tree and the feed all reserve the
 * line's space before it appears.
 */
export default function TheThread({
  orientation = 'horizontal',
  length = DEFAULT_LENGTH,
  testID,
}: TheThreadProps): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  const progress = useRef(new Animated.Value(0)).current;

  useEffect(() => {
    const animation = Animated.loop(
      Animated.timing(progress, {
        toValue: 1,
        duration: motion.durations.max,
        easing: Easing.bezier(...motion.easing.standard),
        useNativeDriver: true,
      }),
    );
    animation.start();

    return () => animation.stop();
  }, [progress]);

  const horizontal = orientation === 'horizontal';
  // Half the length back, scaled down about the centre: the left (or top) edge
  // stays put while the far edge advances.
  const offset = progress.interpolate({
    inputRange: [0, 1],
    outputRange: [-length / 2, 0],
  });
  const opacity = progress.interpolate({
    inputRange: [0, FADE_START, 1],
    outputRange: [1, 1, 0],
  });

  return (
    <View
      style={[
        styles.frame,
        horizontal ? { height: THICKNESS, width: length } : { height: length, width: THICKNESS },
      ]}
      testID={testID}
    >
      <Animated.View
        style={[
          styles.line,
          {
            opacity,
            transform: horizontal
              ? [{ translateX: offset }, { scaleX: progress }]
              : [{ translateY: offset }, { scaleY: progress }],
          },
        ]}
      />
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    frame: {
      alignItems: 'center',
      justifyContent: 'center',
    },
    line: {
      backgroundColor: colors.brand.primary,
      height: '100%',
      width: '100%',
    },
  });

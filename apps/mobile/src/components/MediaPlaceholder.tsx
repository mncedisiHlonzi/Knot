import React, { useEffect, useRef } from 'react';
import { ActivityIndicator, Animated, Easing, Image, StyleSheet, Text, View } from 'react-native';

import { motion, spacing, typeScale, useTheme, useThemedStyles } from '../theme';
import type { ThemeColors } from '../theme';

/**
 * How much the 20px placeholder is blurred when it is stretched over a card.
 *
 * The image is already tiny, so the enlargement is what does most of the
 * softening; ten points of blur is what removes the visible blockiness without
 * turning the picture into a single flat colour, so a reader still sees the
 * composition of what is coming (KNOT-043).
 */
export const LQIP_BLUR_RADIUS = 10;

/**
 * One cycle of the skeleton's pulse. Slow enough to read as "working" rather than
 * as a flashing control, and long enough that the animation never competes with
 * the content that replaces it.
 */
const SHIMMER_DURATION_MS = 900;

/** How faint the shimmer starts and ends, so it breathes instead of blinking. */
const SHIMMER_MIN_OPACITY = 0.12;
const SHIMMER_MAX_OPACITY = 0.4;

type MediaPlaceholderProps = {
  /**
   * The server's `lqip_url`: a ~20px WebP that arrives in about a kilobyte.
   *
   * Null or absent when the row was produced before KNOT-043 (or when the app has
   * nothing at all, which is the post-publish case). The component then draws its
   * animated skeleton instead, so the frame is never empty (KNOT-043).
   */
  readonly lqipUri?: string | null;
  /**
   * `lqip` draws the placeholder picture (or the skeleton when there is none);
   * `skeleton` always draws the skeleton, for media that does not exist yet.
   * Defaults to `lqip`.
   */
  readonly variant?: 'lqip' | 'skeleton';
  /** Whether the small spinner is drawn over the placeholder. Defaults to `true`. */
  readonly spinner?: boolean;
  /** A quiet caption under the spinner, such as "Processing media...". */
  readonly label?: string;
  /** Test identifier, so a screen can find the placeholder it drew. */
  readonly testID?: string;
};

/**
 * The layer a card or the viewer shows before any real media can be drawn.
 *
 * It exists because every rendition — a 400px thumbnail, an 800px poster, a 1200px
 * display, a video's first decoded frame — costs a network round trip, and the
 * frames that used to be drawn in that window were the background colour: white
 * for an image slot, black for a video (the native player's surface). A ~1KB
 * placeholder painted immediately fills that window with a blurred version of the
 * picture, and the skeleton keeps the same promise for media that does not exist
 * yet (KNOT-043).
 *
 * The component is purely presentational: whoever owns the real media decides when
 * to stop drawing it, so the `ready` callback stays with the thing that knows it.
 */
export default function MediaPlaceholder({
  lqipUri,
  variant = 'lqip',
  spinner = true,
  label,
  testID,
}: MediaPlaceholderProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);

  const source = typeof lqipUri === 'string' ? lqipUri.trim() : '';
  const showPicture = variant === 'lqip' && source !== '';
  // No picture to show means the placeholder has to indicate progress by itself:
  // the pulse is what keeps a slow frame from looking like a finished one.
  const showShimmer = !showPicture;

  // A single value drives the pulse, so the animation is one node rather than a
  // tree of them. It is created on every render (the argument to `useRef` is
  // evaluated eagerly) but the ref keeps the first one.
  const pulse = useRef(new Animated.Value(0)).current;

  useEffect(() => {
    if (!showShimmer) {
      return undefined;
    }

    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(pulse, {
          toValue: 1,
          duration: SHIMMER_DURATION_MS,
          easing: Easing.bezier(...motion.easing.standard),
          useNativeDriver: true,
        }),
        Animated.timing(pulse, {
          toValue: 0,
          duration: SHIMMER_DURATION_MS,
          easing: Easing.bezier(...motion.easing.standard),
          useNativeDriver: true,
        }),
      ]),
    );
    loop.start();

    return () => loop.stop();
  }, [pulse, showShimmer]);

  const shimmerOpacity = pulse.interpolate({
    inputRange: [0, 1],
    outputRange: [SHIMMER_MIN_OPACITY, SHIMMER_MAX_OPACITY],
  });

  return (
    <View
      style={styles.container}
      testID={testID}
      // The placeholder stands in for content, so it announces itself as a busy
      // region rather than as an image with no description.
      accessibilityRole="progressbar"
      accessibilityLabel={label ?? 'Media loading'}
    >
      {showPicture ? (
        <Image
          source={{ uri: source }}
          style={styles.fill}
          resizeMode="cover"
          blurRadius={LQIP_BLUR_RADIUS}
          accessibilityIgnoresInvertColors
          testID="media-placeholder-lqip"
        />
      ) : (
        <Animated.View
          style={[styles.fill, styles.shimmer, { opacity: shimmerOpacity }]}
          pointerEvents="none"
          testID="media-placeholder-shimmer"
        />
      )}

      {spinner ? (
        <View style={styles.center} pointerEvents="none">
          <View style={styles.spinnerDisc}>
            <ActivityIndicator color={colors.text.tertiary} size="small" />
          </View>
          {label === undefined ? null : (
            <Text style={[typeScale.caption, styles.label]}>{label}</Text>
          )}
        </View>
      ) : null}
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    center: {
      alignItems: 'center',
      gap: spacing.sm,
      justifyContent: 'center',
      ...StyleSheet.absoluteFillObject,
    },
    container: {
      backgroundColor: colors.bg.sunken,
      ...StyleSheet.absoluteFillObject,
    },
    fill: {
      height: '100%',
      width: '100%',
    },
    label: {
      color: colors.text.tertiary,
    },
    // The disc keeps the spinner legible over a bright placeholder or a pale
    // skeleton, without a panel around it.
    spinnerDisc: {
      alignItems: 'center',
      backgroundColor: colors.scrim.overlay,
      borderRadius: 20,
      height: 40,
      justifyContent: 'center',
      width: 40,
    },
    shimmer: {
      backgroundColor: colors.bg.raised,
    },
  });

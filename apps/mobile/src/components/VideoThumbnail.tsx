import React from 'react';
import { Image, StyleSheet, View } from 'react-native';
import { Play } from 'phosphor-react-native';

import { radius, spacing, useTheme, useThemedStyles } from '../theme';
import type { ThemeColors } from '../theme';

/** The play disc drawn over a video thumbnail, and its glyph. */
const PLAY_DISC_SIZE = 16;
const PLAY_ICON_SIZE = 8;

/** The square's side when a caller does not pass one. */
const DEFAULT_SIZE = 64;

type VideoThumbnailProps = {
  /**
   * A still image representing the video. Optional because the API does not
   * store a poster yet; without one the tile draws a sunken placeholder.
   */
  readonly posterUrl?: string;
  /**
   * The square tile's side, in points. The frame and the media box carry this
   * explicitly rather than sizing by `flex`.
   */
  readonly size?: number;
};

/**
 * A **static** representation of a video for the media strip: a poster image
 * when one exists, otherwise a sunken placeholder, always with a play disc.
 *
 * The strip used to render a paused, muted `<Video>` so its first decoded frame
 * acted as the poster, but Android's native TextureView ignored every layout
 * constraint — including explicit absolute sizing and a clipped, fixed-height
 * strip — and expanded past its 64pt tile, pushing the layout and bleeding into
 * the carousel (KNOT-025-fix8). An `<Image>` or plain `<View>` has no native
 * surface, so it cannot do that (KNOT-025-fix9).
 *
 * The fix8 geometry is preserved exactly: a fixed square frame with
 * `overflow: 'hidden'`, and the media inside an absolutely positioned box with
 * explicit width/height. The carousel is still the only place a video plays.
 */
export default function VideoThumbnail({
  posterUrl,
  size = DEFAULT_SIZE,
}: VideoThumbnailProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  return (
    <View style={[styles.frame, { height: size, width: size }]}>
      {/* The clipped, out-of-flow box from fix8: the still media fills it. */}
      <View
        style={{
          position: 'absolute',
          top: 0,
          left: 0,
          width: size,
          height: size,
        }}
      >
        {posterUrl === undefined ? (
          <View style={styles.placeholder} testID="video-thumbnail-placeholder" />
        ) : (
          <Image
            source={{ uri: posterUrl }}
            style={styles.media}
            resizeMode="cover"
            accessibilityIgnoresInvertColors
          />
        )}
      </View>

      <View style={styles.play}>
        <Play color={colors.text.onBrand} size={PLAY_ICON_SIZE} weight="fill" />
      </View>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    // The square the frame fills. It clips its own corners so the component stays
    // correct even where the caller does not clip it.
    frame: {
      backgroundColor: colors.bg.sunken,
      borderRadius: radius.md,
      overflow: 'hidden',
    },
    // The poster fills the anchor exactly (`cover` crops, never distorts).
    media: {
      height: '100%',
      width: '100%',
    },
    // No poster stored yet: a calm sunken square marks the video item without a
    // native surface that could escape the tile.
    placeholder: {
      backgroundColor: colors.bg.sunken,
      height: '100%',
      width: '100%',
    },
    play: {
      alignItems: 'center',
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.pill,
      bottom: spacing.xs,
      height: PLAY_DISC_SIZE,
      justifyContent: 'center',
      position: 'absolute',
      right: spacing.xs,
      width: PLAY_DISC_SIZE,
    },
  });

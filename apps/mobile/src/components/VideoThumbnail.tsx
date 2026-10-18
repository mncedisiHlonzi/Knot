import React, { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { Play } from 'phosphor-react-native';
import Video from 'react-native-video';

import { colors, radius, spacing } from '../theme';

/** The play disc drawn over a video thumbnail, and its glyph. */
const PLAY_DISC_SIZE = 16;
const PLAY_ICON_SIZE = 8;

/** The square's side when a caller does not pass one. */
const DEFAULT_SIZE = 64;

type VideoThumbnailProps = {
  /** The resolved, absolute URL of the video. */
  readonly uri: string;
  /**
   * The square tile's side, in points. The player is sized **explicitly** rather
   * than by `flex`, so the native video surface can never take its layout from
   * the clip's natural size — the stretched, growing tiles the strip showed
   * (KNOT-025-fix5).
   */
  readonly size?: number;
};

/**
 * A video's first frame, used as a static strip thumbnail.
 *
 * `react-native-video` has no API that exports a decoded frame as an image, so
 * this draws a **paused, muted player**: both native players render their first
 * decoded frame on the video surface, which is exactly the poster the API does
 * not store. `onLoad` and `onReadyForDisplay` (the first-frame-rendered event)
 * retire the sunken placeholder; if the source cannot be decoded neither fires,
 * so the placeholder stays and the strip never shows a blank tile. The play disc
 * marks the item as a video. The strip is deliberately static — the main carousel
 * is the only place a video plays (KNOT-025-fix4).
 *
 * `useTextureView` keeps the surface inside the tile's rounded, clipped box on
 * Android — a SurfaceView punches through the RN hierarchy and ignores the clip
 * (the same rule as the map surfaces).
 *
 * The square is a **fixed, explicit size**: the frame carries the tile's
 * width/height and the player fills it at `100%` with `cover`, so the frame's
 * shape — not the clip's — decides the surface's size (KNOT-025-fix5).
 *
 * The player also sits inside an **absolute-fill** view. That takes it out of
 * layout flow and gives the native surface a box with all four insets resolved,
 * so it cannot grow past the frame; the frame's `overflow: 'hidden'` then clips
 * it. Without the anchor an Android video surface measured itself and bled up
 * into the carousel when its tile was active (KNOT-025-fix6).
 */
export default function VideoThumbnail({
  uri,
  size = DEFAULT_SIZE,
}: VideoThumbnailProps): React.ReactElement {
  const [ready, setReady] = useState(false);
  const handleReady = (): void => setReady(true);

  return (
    <View style={[styles.frame, { height: size, width: size }]}>
      {/* The clipping anchor: a full-bleed, out-of-flow box the player fills
          exactly (KNOT-025-fix6). */}
      <View style={StyleSheet.absoluteFillObject}>
        <Video
          source={{ uri }}
          style={styles.video}
          paused
          muted
          repeat={false}
          resizeMode="cover"
          useTextureView
          onLoad={handleReady}
          onReadyForDisplay={handleReady}
        />
      </View>

      {ready ? null : <View style={styles.fallback} testID="video-thumbnail-fallback" />}

      <View style={styles.play}>
        <Play color={colors.text.onBrand} size={PLAY_ICON_SIZE} weight="fill" />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  // The square the frame fills. It clips its own corners so the component stays
  // correct even where the caller does not clip it.
  frame: {
    backgroundColor: colors.bg.sunken,
    borderRadius: radius.md,
    overflow: 'hidden',
  },
  // Painted above the video until the first frame is ready, so the tile is a calm
  // sunken square rather than a black flash while the decoder starts.
  fallback: {
    backgroundColor: colors.bg.sunken,
    bottom: 0,
    left: 0,
    position: 'absolute',
    right: 0,
    top: 0,
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
  // `cover` plus a full-size style: the frame's square wins, and the video's own
  // aspect ratio is cropped rather than distorted.
  video: {
    height: '100%',
    width: '100%',
  },
});

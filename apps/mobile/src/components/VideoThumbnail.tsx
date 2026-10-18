import React, { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import Video from 'react-native-video';

import { colors, radius, spacing, tabularNums, typeScale } from '../theme';
import { formatDuration } from './feedVideoMeta';

type VideoThumbnailProps = {
  /** The resolved, absolute URL of the video. */
  readonly uri: string;
  /**
   * Whether the tile is the one allowed to play. The strip passes `true` only for
   * the carousel's active item while the strip is on screen and no viewer is open,
   * so a video preview never keeps decoding in the background (KNOT-025-fix3).
   */
  readonly isVisible: boolean;
  /**
   * The duration the payload already knows, in milliseconds. It seeds the badge
   * before the player has loaded; omitted or non-positive values draw no badge.
   */
  readonly durationMs?: number | null;
};

/**
 * A small, looping video preview for the thumbnail strip.
 *
 * The video is always **muted** and loops. Playback is driven entirely by
 * `isVisible`: the strip pauses a preview when its item is not the carousel's
 * active one, when the strip has scrolled off the screen, or when the viewer or
 * report sheet is open. That is one player at a time, which is the performance
 * rule the Feed's viewability pattern exists for.
 *
 * `react-native-video` has no API that exports a frame as an image, so the video
 * surface itself is the thumbnail. `onLoad` / `onReadyForDisplay` retire a sunken
 * placeholder once there is something to show; if the source cannot be decoded,
 * the placeholder stays. `useTextureView` keeps the surface inside the tile's
 * rounded, clipped box on Android — a SurfaceView punches through the RN
 * hierarchy and ignores the clip (the same rule as the map surfaces).
 */
export default function VideoThumbnail({
  uri,
  isVisible,
  durationMs,
}: VideoThumbnailProps): React.ReactElement {
  const [ready, setReady] = useState(false);
  const handleReady = (): void => setReady(true);

  const durationLabel =
    typeof durationMs === 'number' && Number.isFinite(durationMs) && durationMs > 0
      ? formatDuration(durationMs / 1000)
      : '';

  return (
    <View style={styles.container}>
      <Video
        source={{ uri }}
        style={styles.video}
        paused={!isVisible}
        muted
        repeat
        resizeMode="cover"
        useTextureView
        onLoad={handleReady}
        onReadyForDisplay={handleReady}
      />

      {ready ? null : <View style={styles.fallback} testID="video-thumbnail-fallback" />}

      {durationLabel === '' ? null : (
        <View style={styles.duration} pointerEvents="none">
          <Text style={[typeScale.caption, tabularNums, styles.durationText]}>{durationLabel}</Text>
        </View>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  duration: {
    backgroundColor: colors.scrim.overlay,
    borderRadius: radius.sm,
    bottom: spacing.xs,
    left: spacing.xs,
    paddingHorizontal: spacing.xs,
    paddingVertical: 1,
    position: 'absolute',
  },
  durationText: {
    color: colors.text.onBrand,
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
  video: {
    bottom: 0,
    left: 0,
    position: 'absolute',
    right: 0,
    top: 0,
  },
});

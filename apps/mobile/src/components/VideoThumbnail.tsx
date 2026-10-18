import React, { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { Play } from 'phosphor-react-native';
import Video from 'react-native-video';

import { colors, radius, spacing } from '../theme';

/** The play disc drawn over a video thumbnail, and its glyph. */
const PLAY_DISC_SIZE = 16;
const PLAY_ICON_SIZE = 8;

type VideoThumbnailProps = {
  /** The resolved, absolute URL of the video. */
  readonly uri: string;
};

/**
 * A video's first frame, used as a strip thumbnail.
 *
 * `react-native-video` has no API that exports a decoded frame as an image, so
 * this draws a **paused, muted player**: both native players render their first
 * decoded frame on the video surface, which is exactly the poster the API does
 * not store. `onLoad` and `onReadyForDisplay` (the first-frame-rendered event)
 * retire the sunken placeholder; if the source cannot be decoded neither fires,
 * so the placeholder stays and the strip never shows a blank tile. The play disc
 * marks the item as a video.
 *
 * It is deliberately a separate component from `FeedVideo`: a thumbnail needs no
 * duration, mute control, autoplay or viewability state, and mounting the full
 * player chrome ten times in a strip would be both noisy and wasteful.
 */
export default function VideoThumbnail({ uri }: VideoThumbnailProps): React.ReactElement {
  const [loaded, setLoaded] = useState(false);
  const handleReady = (): void => setLoaded(true);

  return (
    <View style={styles.container}>
      <Video
        source={{ uri }}
        style={styles.video}
        paused
        muted
        repeat={false}
        resizeMode="cover"
        // A TextureView (rather than ExoPlayer's default SurfaceView) keeps the
        // video inside the tile's rounded, clipped box on Android. A SurfaceView
        // punches through the RN hierarchy, ignores the parent's clipping and
        // would paint over the play disc — the same rule as the map surfaces.
        useTextureView
        onLoad={handleReady}
        onReadyForDisplay={handleReady}
      />

      {loaded ? null : <View style={styles.fallback} testID="video-thumbnail-fallback" />}

      <View style={styles.play}>
        <Play color={colors.text.onBrand} size={PLAY_ICON_SIZE} weight="fill" />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
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
  video: {
    bottom: 0,
    left: 0,
    position: 'absolute',
    right: 0,
    top: 0,
  },
});

/**
 * The player inside the full-screen viewer's modal (KNOT-051, KNOT-050).
 *
 * Inline surfaces own their `<Video>` again — a feed card's player lives in the card
 * and scrolls with it — so this component no longer draws a floating player over the
 * screens. What is left is the one case that cannot be inline: a React Native
 * `Modal` is a separate window on both platforms, so a view from the app's window
 * cannot paint over it. The viewer's picture is therefore re-seated here, inside the
 * modal, at the playhead the inline card published to the engine.
 *
 * It draws only while the engine is in `fullscreen` mode, which is what keeps a
 * single player in the modal window and none at all anywhere else.
 *
 * The host is deliberately intangible (`pointerEvents="none"`): the native video
 * surface is the topmost thing over the picture on Android, and a tap that landed
 * on it would never reach the viewer's own tap layer (KNOT-020k-fix5).
 */
import React, { useCallback, useRef } from 'react';
import { StyleSheet, View } from 'react-native';
import Video, { type OnLoadData } from 'react-native-video';

import { useMediaPlayerActions, useMediaSurface } from '../context/MediaPlayerContext';
import type { MediaSurfaceHandle } from '../context/MediaPlayerContext';
import type { MediaFrame } from '../context/mediaPlayerState';
import { useTheme } from '../theme';

type MediaHostProps = {
  /** Test identifier for the host box. */
  readonly testID?: string;
};

/**
 * The viewer's player: it draws the clip the engine is presenting full-screen.
 */
export default function MediaHost({ testID }: MediaHostProps): React.ReactElement | null {
  const surface = useMediaSurface();
  const frame = surface.frame;
  const uri = surface.uri;

  // Only the theatre draws, and only while it has somewhere to draw.
  if (uri === null || frame === null || surface.mode !== 'fullscreen') {
    return null;
  }

  return (
    <MediaSurfaceView surface={surface} frame={frame} testID={testID ?? 'media-host-viewer'} />
  );
}

type MediaSurfaceViewProps = {
  readonly surface: MediaSurfaceHandle;
  readonly frame: MediaFrame;
  readonly testID: string;
};

/**
 * The player itself, mounted and unmounted with its frame.
 *
 * It captures the playhead on mount and applies it when the source is ready, so
 * opening the viewer mid-clip resumes exactly where the card had reached instead
 * of starting at 0:00. No seek is issued for a clip that starts at zero.
 */
function MediaSurfaceView({ surface, frame, testID }: MediaSurfaceViewProps): React.ReactElement {
  const { colors } = useTheme();
  const player = useMediaPlayerActions();
  // The engine's position at the moment this native player was mounted. A
  // re-seat mid-clip therefore resumes exactly where the previous host stopped.
  const resumeAt = useRef<number>(player.getCurrentTime());
  const seekApplied = useRef(false);

  const handleLoad = useCallback(
    (data: OnLoadData): void => {
      surface.onLoad(data);

      if (seekApplied.current) {
        return;
      }
      seekApplied.current = true;

      const target = resumeAt.current;
      if (target > 0) {
        surface.videoRef.current?.seek(target);
      }
    },
    [surface],
  );

  return (
    <View
      pointerEvents="none"
      style={[
        styles.host,
        {
          height: frame.height,
          left: frame.x,
          top: frame.y,
          width: frame.width,
        },
      ]}
      testID={testID}
    >
      <Video
        ref={surface.videoRef}
        source={{ uri: surface.uri ?? '' }}
        style={styles.video}
        paused={surface.paused}
        muted={surface.muted}
        repeat={surface.loop}
        // The theatre letterboxes: a viewer has paid for the whole picture and
        // cropping it would lose it.
        resizeMode="contain"
        // A `SurfaceView` (the library's Android default) is a separate window
        // layer: it punches through the React Native hierarchy, so it ignores its
        // parent's clipping and paints over whatever sits above it. A `TextureView`
        // is an ordinary view, so the picture stays inside its box and stacks like
        // every other element (KNOT-025-fix6, KNOT-048-fix2).
        useTextureView
        progressUpdateInterval={250}
        // The native surface is opaque and empty until the first frame decodes, so
        // the player is asked for the surround it will sit in: the theatre's black
        // (KNOT-045, KNOT-051).
        shutterColor={colors.bg.theater}
        onLoad={handleLoad}
        onProgress={surface.onProgress}
        testID="media-player-viewer"
      />
    </View>
  );
}

const styles = StyleSheet.create({
  host: {
    position: 'absolute',
  },
  video: {
    height: '100%',
    width: '100%',
  },
});

/**
 * The single host for the unified media engine (KNOT-051).
 *
 * There is exactly one `<Video>` mounted at any moment, and it lives here — not
 * inside a feed card, and not inside the viewer. `MediaHost` reads the engine's
 * surface handle and draws the player at the rectangle the active surface handed
 * it: a feed card's media frame, a carousel page, or the whole theatre.
 *
 * Two placements exist because a React Native `Modal` is a separate window on
 * both platforms: a view from the app's window cannot paint over it, so the player
 * must be re-seated inside the modal while it is open. The engine guarantees only
 * one of the two placements mounts the player, and the position is carried over —
 * see `MediaPlayerContext`.
 *
 * The host is deliberately intangible (`pointerEvents="none"`): the native video
 * surface is the topmost thing over the picture on Android, and a tap that landed
 * on it would never reach the card or the viewer's own tap layer
 * (KNOT-020k-fix5).
 */
import React, { useCallback, useRef } from 'react';
import { StyleSheet, View } from 'react-native';
import Video, { type OnLoadData } from 'react-native-video';

import { useMediaPlayerActions, useMediaSurface } from '../context/MediaPlayerContext';
import type { MediaSurfaceHandle } from '../context/MediaPlayerContext';
import type { MediaFrame } from '../context/mediaPlayerState';
import { useTheme } from '../theme';

/** Where the host is mounted: the app's window, or the viewer's modal. */
export type MediaHostPlacement = 'root' | 'viewer';

type MediaHostProps = {
  /** Defaults to `root`. The viewer passes `viewer`. */
  readonly placement?: MediaHostPlacement;
  /** Test identifier for the host box. */
  readonly testID?: string;
};

/** The mode a placement is allowed to draw. */
function modeFor(placement: MediaHostPlacement): 'inline' | 'fullscreen' {
  return placement === 'viewer' ? 'fullscreen' : 'inline';
}

/**
 * The host at the root of the app: it draws the picture over whatever screen is
 * showing, at the frame the active surface reported.
 */
export default function MediaHost({
  placement = 'root',
  testID,
}: MediaHostProps): React.ReactElement | null {
  const surface = useMediaSurface();
  const frame = surface.frame;
  const uri = surface.uri;

  // Only one placement may draw, and only while it has somewhere to draw.
  if (uri === null || frame === null || surface.mode !== modeFor(placement)) {
    return null;
  }

  return (
    <MediaSurfaceView
      surface={surface}
      frame={frame}
      placement={placement}
      testID={testID ?? `media-host-${placement}`}
    />
  );
}

type MediaSurfaceViewProps = {
  readonly surface: MediaSurfaceHandle;
  readonly frame: MediaFrame;
  readonly placement: MediaHostPlacement;
  readonly testID: string;
};

/**
 * The player itself, mounted and unmounted with its frame.
 *
 * It captures the playhead on mount and applies it when the source is ready.
 * That is what makes a *re-seat* seamless: the viewer's modal window mounts a new
 * native player, and it resumes at the second the engine holds instead of 0:00.
 * No seek is issued for a clip that starts at zero, and only the first load of a
 * given mount is affected.
 */
function MediaSurfaceView({
  surface,
  frame,
  placement,
  testID,
}: MediaSurfaceViewProps): React.ReactElement {
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
        // Inline surfaces fill their frame; the theatre letterboxes, because a
        // viewer has paid for the whole picture and cropping it would lose it.
        resizeMode={placement === 'viewer' ? 'contain' : 'cover'}
        // A `SurfaceView` (the library's Android default) is a separate window
        // layer: it punches through the React Native hierarchy, so it ignores its
        // parent's clipping and paints over whatever sits above it -- the card's own
        // chrome, the floating header, a modal. A `TextureView` is an ordinary view,
        // so the picture stays inside the rectangle the engine reported and stacks
        // like every other element (KNOT-025-fix6, KNOT-048-fix2).
        useTextureView
        progressUpdateInterval={250}
        // The native surface is opaque and empty until the first frame decodes, so
        // the player is asked for the surround it will sit in: the app's canvas on
        // a card, the theatre's black in the viewer (KNOT-045, KNOT-051).
        shutterColor={placement === 'viewer' ? colors.bg.theater : colors.bg.canvas}
        onLoad={handleLoad}
        onProgress={surface.onProgress}
        testID={`media-player-${placement}`}
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

/**
 * The media engine: one shared state machine, one player per window (KNOT-051).
 *
 * Every inline surface owns its own `<Video>` again (KNOT-050): a feed card, a
 * carousel page and the profile wall's tile all mount a player inside their own
 * layout, so the platform scrolls the picture with the card and nothing has to be
 * measured or repositioned while the list moves. What the engine still owns is the
 * *shared* part — which media is on screen, where it has reached, whether it is
 * muted — so opening the full-screen viewer on the clip the feed is playing carries
 * on from that second instead of restarting it.
 *
 * ## Why the viewer still gets a player from here
 *
 * A React Native `Modal` is its own window on both platforms (an Android
 * `Dialog`, an iOS presented controller), so a view from the app's window cannot
 * paint over it: the inline player has to be unmounted and the picture re-seated
 * inside the modal. `MediaHost` does that re-seat from the engine's state, at the
 * playhead the inline card published through `reportProgress`. There is therefore
 * exactly one player in the app's window (the settled card) and one in the modal,
 * never two in the same window.
 *
 * ## The three contexts
 *
 * - **Actions** never change identity, so a screen can depend on them without
 *   re-rendering when playback moves.
 * - **Playback** changes on every progress tick; only the components that draw a
 *   position (the badge, the transport) subscribe to it.
 * - **Surface** is what the viewer's `MediaHost` draws from.
 */
import React, { createContext, useCallback, useContext, useMemo, useReducer, useRef } from 'react';
import { StyleSheet, View } from 'react-native';
import Video, { type OnLoadData, type OnProgressData } from 'react-native-video';

import { useThemedStyles } from '../theme';
import type { ThemeColors } from '../theme';
import { cachedVideoUri } from '../utils/videoCache';
import {
  initialMediaPlayerState,
  mediaPlayerReducer,
  seekTargetFor,
  type MediaFrame,
  type MediaPlaybackMode,
  type MediaPlayerState,
  type PlayMediaRequest,
} from './mediaPlayerState';

/** The native player, as the engine holds it. */
export type MediaVideoRef = React.ComponentRef<typeof Video>;

/**
 * What a surface can ask the engine to do.
 *
 * These are the methods the task requires (`playMedia`, `pauseMedia`,
 * `getCurrentTime`, `getActiveMediaId`) plus what the chrome needs to drive the
 * one player: seeking, muting and play/pause.
 */
export type MediaPlayerActions = {
  /**
   * Plays `request.id`. When it is already the active media the clip is NOT
   * reloaded: only its presentation changes, which is what makes the viewer open
   * where the feed left off.
   */
  readonly playMedia: (request: PlayMediaRequest) => void;
  /** Stops the clip and hands the surface back to the screens. */
  readonly pauseMedia: (id?: string) => void;
  /** Starts the active clip again without reloading it. */
  readonly resumeMedia: (id?: string) => void;
  /** Flips play/pause for the active clip. */
  readonly togglePlayback: (id?: string) => void;
  /** Seeks the active clip, in seconds. */
  readonly seekTo: (seconds: number) => void;
  /** Mutes or unmutes the one player. */
  readonly setMuted: (muted: boolean) => void;
  /** Hides the surface while a pager shows a photo, without stopping the clip. */
  readonly clearSurface: (id?: string) => void;
  /** Stops the clip and hands the surface back to the screens (viewer closed). */
  readonly releaseMedia: () => void;
  /** The playhead, read live rather than from the last render. */
  readonly getCurrentTime: () => number;
  /** The media on screen, read live. */
  readonly getActiveMediaId: () => string | null;
  /**
   * Records where an inline card's OWN player has reached, so the viewer can open
   * the same clip at that second.
   *
   * Inline surfaces own a `<Video>` again (KNOT-050): the card's player scrolls with
   * the card and nothing measures anything, so the engine can no longer read a
   * playhead from the one host. The card publishes what its player is doing instead;
   * the engine keeps the answer the viewer's re-seat resumes from.
   */
  readonly reportProgress: (id: string, currentTime: number) => void;
  /** Records the duration an inline card's own player reported. */
  readonly reportLoaded: (id: string, duration: number) => void;
};

/**
 * What `MediaHost` needs to draw the one player.
 *
 * It carries the native ref itself, so the engine and the host act on the same
 * player, and the frame, which changes on every scroll tick.
 */
export type MediaSurfaceHandle = {
  readonly frame: MediaFrame | null;
  readonly mode: MediaPlaybackMode;
  readonly uri: string | null;
  readonly paused: boolean;
  readonly muted: boolean;
  readonly loop: boolean;
  readonly videoRef: React.RefObject<MediaVideoRef>;
  readonly onLoad: (data: OnLoadData) => void;
  readonly onProgress: (data: OnProgressData) => void;
};

const MediaPlayerActionsContext = createContext<MediaPlayerActions | null>(null);
const MediaPlaybackContext = createContext<MediaPlayerState | null>(null);
const MediaSurfaceContext = createContext<MediaSurfaceHandle | null>(null);

/**
 * The engine, mounted once at the root of the app.
 *
 * It renders no player of its own: `MediaHost` does, wherever the active
 * presentation lives. The provider's only output is a container `View` that every
 * host is measured against, so a frame reported by the Feed and a frame reported
 * by the carousel are in the same coordinate space.
 */
export function MediaPlayerProvider({
  children,
}: {
  readonly children: React.ReactNode;
}): React.ReactElement {
  const [state, dispatch] = useReducer(mediaPlayerReducer, initialMediaPlayerState);
  // The live state, so the imperative API (`getCurrentTime`, a seek target) never
  // reads a value a render has not committed yet.
  const stateRef = useRef<MediaPlayerState>(state);
  stateRef.current = state;

  const videoRef = useRef<MediaVideoRef | null>(null);
  /** A position that must be applied once the next source has loaded. */
  const pendingSeek = useRef<number | null>(null);

  const playMedia = useCallback((request: PlayMediaRequest): void => {
    const target = seekTargetFor(stateRef.current, request);
    dispatch({ type: 'play', request });

    if (target === null) {
      return;
    }

    // A seek is only meaningful on the player that is already holding this clip.
    // For a new clip the surface mounts fresh, so the position is applied when its
    // metadata arrives.
    const sameLoadedClip = stateRef.current.activeMediaId === request.id && stateRef.current.ready;
    if (sameLoadedClip) {
      videoRef.current?.seek(target);
      dispatch({ type: 'seek', id: request.id, currentTime: target });
    } else {
      pendingSeek.current = target;
    }
  }, []);

  const pauseMedia = useCallback((id?: string): void => {
    dispatch({ type: 'pause', id });
  }, []);

  const resumeMedia = useCallback((id?: string): void => {
    dispatch({ type: 'resume', id });
  }, []);

  const togglePlayback = useCallback((id?: string): void => {
    dispatch({ type: 'togglePlayback', id });
  }, []);

  const seekTo = useCallback((seconds: number): void => {
    if (typeof seconds !== 'number' || !Number.isFinite(seconds) || seconds < 0) {
      return;
    }
    const id = stateRef.current.activeMediaId;
    if (id === null) {
      return;
    }
    videoRef.current?.seek(seconds);
    dispatch({ type: 'seek', id, currentTime: seconds });
  }, []);

  const setMuted = useCallback((muted: boolean): void => {
    dispatch({ type: 'muted', muted });
  }, []);

  const clearSurface = useCallback((id?: string): void => {
    dispatch({ type: 'clearSurface', id });
  }, []);

  const releaseMedia = useCallback((): void => {
    dispatch({ type: 'release' });
  }, []);

  const getCurrentTime = useCallback((): number => stateRef.current.currentTime, []);
  const getActiveMediaId = useCallback((): string | null => stateRef.current.activeMediaId, []);

  const onLoad = useCallback((data: OnLoadData): void => {
    const id = stateRef.current.activeMediaId ?? undefined;
    if (id !== undefined) {
      dispatch({ type: 'loaded', id, duration: data.duration });

      const natural = data.naturalSize;
      if (
        typeof natural?.width === 'number' &&
        typeof natural?.height === 'number' &&
        natural.width > 0 &&
        natural.height > 0
      ) {
        dispatch({
          type: 'naturalSize',
          id,
          size: { width: natural.width, height: natural.height },
        });
      }
    }

    const target = pendingSeek.current;
    if (target !== null) {
      pendingSeek.current = null;
      videoRef.current?.seek(target);
      if (id !== undefined) {
        dispatch({ type: 'seek', id, currentTime: target });
      }
    }
  }, []);

  const onProgress = useCallback((data: OnProgressData): void => {
    const id = stateRef.current.activeMediaId;
    if (id === null) {
      return;
    }
    dispatch({ type: 'progress', id, currentTime: data.currentTime });
  }, []);

  /**
   * What an inline card's own player is doing (KNOT-050).
   *
   * The card owns the `<Video>` now, so the engine cannot observe it; the card tells
   * it where the clip has reached, and the viewer reads that back when it opens on
   * the same media. The reducer ignores a report that names a media which is no
   * longer active, so a card being scrolled away can never move the clip that
   * replaced it.
   */
  const reportProgress = useCallback((id: string, currentTime: number): void => {
    dispatch({ type: 'progress', id, currentTime });
  }, []);

  const reportLoaded = useCallback((id: string, duration: number): void => {
    dispatch({ type: 'loaded', id, duration });
  }, []);

  const actions = useMemo<MediaPlayerActions>(
    () => ({
      playMedia,
      pauseMedia,
      resumeMedia,
      togglePlayback,
      seekTo,
      setMuted,
      clearSurface,
      releaseMedia,
      getCurrentTime,
      getActiveMediaId,
      reportProgress,
      reportLoaded,
    }),
    [
      clearSurface,
      getActiveMediaId,
      getCurrentTime,
      pauseMedia,
      playMedia,
      releaseMedia,
      reportLoaded,
      reportProgress,
      resumeMedia,
      seekTo,
      setMuted,
      togglePlayback,
    ],
  );

  // The local cache proxy is applied once per source: a second scroll past the
  // same clip then re-reads it from the device instead of downloading it again
  // (KNOT-037-fix10).
  const sourceUri = useMemo(
    () => (state.uri === null ? null : cachedVideoUri(state.uri)),
    [state.uri],
  );

  const surface = useMemo<MediaSurfaceHandle>(
    () => ({
      frame: state.frame,
      mode: state.mode,
      uri: sourceUri,
      paused: !state.playing,
      muted: state.muted,
      loop: state.loop,
      videoRef,
      onLoad,
      onProgress,
    }),
    [
      onLoad,
      onProgress,
      sourceUri,
      state.frame,
      state.loop,
      state.mode,
      state.muted,
      state.playing,
    ],
  );

  const styles = useThemedStyles(createStyles);

  return (
    <MediaPlayerActionsContext.Provider value={actions}>
      <MediaPlaybackContext.Provider value={state}>
        <MediaSurfaceContext.Provider value={surface}>
          <View style={styles.root} pointerEvents="box-none">
            {children}
          </View>
        </MediaSurfaceContext.Provider>
      </MediaPlaybackContext.Provider>
    </MediaPlayerActionsContext.Provider>
  );
}

/** The engine's imperative API. Stable for the life of the app. */
export function useMediaPlayerActions(): MediaPlayerActions {
  const actions = useContext(MediaPlayerActionsContext);
  if (actions === null) {
    throw new Error('useMediaPlayerActions must be used inside a MediaPlayerProvider.');
  }
  return actions;
}

/** The live playback state. Subscribe only where a position or mode is drawn. */
export function useMediaPlayback(): MediaPlayerState {
  const playback = useContext(MediaPlaybackContext);
  if (playback === null) {
    throw new Error('useMediaPlayback must be used inside a MediaPlayerProvider.');
  }
  return playback;
}

/** The surface geometry. Only `MediaHost` should read this. */
export function useMediaSurface(): MediaSurfaceHandle {
  const surface = useContext(MediaSurfaceContext);
  if (surface === null) {
    throw new Error('useMediaSurface must be used inside a MediaPlayerProvider.');
  }
  return surface;
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    root: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
  });

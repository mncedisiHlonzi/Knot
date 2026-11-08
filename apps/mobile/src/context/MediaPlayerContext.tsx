/**
 * The unified media engine (KNOT-051).
 *
 * Before this module there were three players: one inside `FeedVideo` (the Feed,
 * Place Stories, the profile wall and the Story Detail carousel), and a second
 * inside `VideoPlayer` for the full-screen viewer. Opening the viewer therefore
 * stopped one native player and started another, which restarted the clip at
 * 0:00 and put two decoders on the hardware for a moment.
 *
 * Now there is **one engine**. It owns the state machine
 * (`mediaPlayerState.ts`), the one native player ref, and the rectangle the
 * player's surface occupies. Surfaces are *hosts*: `FeedVideo`, the carousel page
 * and the viewer's video page call `playMedia` and hand the engine a `View` to
 * draw over. Opening the viewer on the clip that is already playing keeps the
 * same media id, so the engine changes only where the picture is shown — the
 * position is untouched, and the viewer opens at the second the feed had reached.
 *
 * ## Why there are two host placements
 *
 * A React Native `Modal` is its own window on both platforms (an Android
 * `Dialog`, an iOS presented controller). A view in the app's window can never
 * draw over it, so the native player *view* cannot be carried into the viewer;
 * only the engine's state can. The engine therefore allows exactly one host to
 * render the player at a time (`root` for the screens, `viewer` for the modal),
 * and the viewer re-seats the picture at the position the engine holds. That is
 * the only platform-compatible form of player swapping — and it is still one
 * player, one source of truth, and one decoder.
 *
 * ## The three contexts
 *
 * - **Actions** never change identity, so a screen can depend on them without
 *   re-rendering when playback moves.
 * - **Playback** changes on every progress tick; only the components that draw a
 *   position (the badge, the transport) subscribe to it.
 * - **Surface** changes on every measured frame, so that a scrolling list can
 *   keep the picture glued to its card without re-rendering a single row: only
 *   `MediaHost` reads it.
 */
import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useReducer,
  useRef,
} from 'react';
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

/** A host component's node, as React Native measures it. */
type HostNode = React.ComponentRef<typeof View>;

/**
 * How long a registration waits before measuring itself a second time.
 *
 * Long enough for the engine's own play dispatch and the native layout of a
 * freshly mounted card to land, short enough to be invisible (KNOT-048-fix2).
 */
const MEASURE_RETRY_MS = 120;

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
  /** Registers the view the engine should draw the picture over. */
  readonly registerHost: (id: string, node: HostNode | null) => void;
  /** Forgets a host that is unmounting or no longer playing its media. */
  readonly releaseHost: (id: string) => void;
  /** Re-measures the registered host, so a scrolling list keeps the picture in place. */
  readonly measureHost: () => void;
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

  const containerRef = useRef<HostNode | null>(null);
  const videoRef = useRef<MediaVideoRef | null>(null);
  /** A position that must be applied once the next source has loaded. */
  const pendingSeek = useRef<number | null>(null);
  /** The host the engine currently draws over, and the media it belongs to. */
  const hostRef = useRef<HostNode | null>(null);
  const hostMediaId = useRef<string | null>(null);
  /** The deferred re-measure a registration scheduled, if any. */
  const measureRetry = useRef<ReturnType<typeof setTimeout> | null>(null);

  /**
   * Re-measures the registered host in the container's coordinate space.
   *
   * Both rectangles are read with `measureInWindow` and subtracted, rather than
   * asking the node to measure itself against the container. `measureLayout` walks
   * the parent chain, and on Android it does not resolve a node nested inside a
   * horizontal `ScrollView` — the Story Detail's pager — returning a placeholder
   * rectangle a couple of pixels across. The picture then has nowhere to draw, which
   * is the empty media area with a running clock the founder saw on an opened story
   * (KNOT-048-fix2). A difference of two window rectangles is independent of anywhere
   * the node is nested, and it still accounts for the status bar, because the
   * container's own origin is measured the same way.
   */
  const measureHost = useCallback((): void => {
    const node = hostRef.current;
    const container = containerRef.current;
    const id = stateRef.current.activeMediaId;
    if (node === null || container === null || id === null) {
      return;
    }
    // A host only owns the surface while the picture is inline; the viewer's
    // modal window cannot share it, so its frame is set explicitly instead.
    if (stateRef.current.mode !== 'inline' || hostMediaId.current !== id) {
      return;
    }

    const measurable = node as unknown as {
      measureInWindow?: (
        onSuccess: (x: number, y: number, width: number, height: number) => void,
      ) => void;
    };
    const rootMeasurable = container as unknown as {
      measureInWindow?: (
        onSuccess: (x: number, y: number, width: number, height: number) => void,
      ) => void;
    };
    const nodeMeasure = measurable.measureInWindow;
    const rootMeasure = rootMeasurable.measureInWindow;
    if (typeof nodeMeasure !== 'function' || typeof rootMeasure !== 'function') {
      return;
    }

    try {
      nodeMeasure.call(node, (x, y, width, height) => {
        if (!(width > 0) || !(height > 0)) {
          return;
        }
        rootMeasure.call(container, (rootX, rootY) => {
          dispatch({
            type: 'frame',
            id,
            frame: { x: x - rootX, y: y - rootY, width, height },
          });
        });
      });
    } catch {
      // No native layout yet (the view is not attached). The next `onLayout` or
      // scroll tick measures again.
    }
  }, []);

  const registerHost = useCallback(
    (id: string, node: HostNode | null): void => {
      if (node === null) {
        if (hostMediaId.current === id) {
          hostRef.current = null;
          hostMediaId.current = null;
        }
        return;
      }
      hostRef.current = node;
      hostMediaId.current = id;
      measureHost();

      // A registration can land before the engine has committed the play that makes
      // this the active surface — and a screen that comes back to the clip it was
      // already playing (returning from Settings, say) changes NO engine state, so
      // nothing would ever retry the measurement. The picture would then stay
      // undrawn, which reads as an empty media frame. One deferred measure covers
      // both cases; it is cleared when another host registers first (KNOT-048-fix2).
      if (measureRetry.current !== null) {
        clearTimeout(measureRetry.current);
      }
      measureRetry.current = setTimeout(() => {
        measureRetry.current = null;
        measureHost();
      }, MEASURE_RETRY_MS);
    },
    [measureHost],
  );

  const releaseHost = useCallback((id: string): void => {
    if (hostMediaId.current !== id) {
      return;
    }
    hostRef.current = null;
    hostMediaId.current = null;
  }, []);

  /**
   * Geometry follows the engine's own transitions.
   *
   * A surface measures itself when it registers, but a registration can land while
   * the engine is not ready to measure — the viewer still owns the surface, or the
   * screen is mid-transition — and then nothing would retry it. The picture would
   * simply not be drawn (its rectangle was cleared with the old surface), which is
   * what an empty media frame with a running clock looks like on device
   * (KNOT-048-fix2). Re-measuring after the engine commits a new mode, media or
   * play state is the retry, and it costs one `measureLayout` per transition.
   */
  useEffect(() => {
    measureHost();
  }, [measureHost, state.activeMediaId, state.mode, state.playing]);

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
      registerHost,
      releaseHost,
      measureHost,
    }),
    [
      clearSurface,
      getActiveMediaId,
      getCurrentTime,
      measureHost,
      pauseMedia,
      playMedia,
      registerHost,
      releaseMedia,
      releaseHost,
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
          <View ref={containerRef} style={styles.root} pointerEvents="box-none">
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

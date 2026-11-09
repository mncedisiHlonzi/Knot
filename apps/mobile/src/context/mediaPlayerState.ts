/**
 * The media player's state machine (KNOT-051).
 *
 * The engine has one rule above all others: **one clip, one position**. A surface
 * — the Feed, the Story Detail carousel, the full-screen viewer — never owns a
 * player of its own; it asks this state machine to play a media, and the machine
 * decides whether that means "start something new" or "carry on where we were".
 *
 * That distinction is the whole of the "Continue Playing" feature. Opening the
 * viewer on the clip that is already playing must NOT reset it, so a `play` for
 * the media that is already active keeps `currentTime` and only changes the
 * presentation (mode, frame, mute). A `play` for a different media starts at the
 * requested position, or at zero.
 *
 * It is deliberately pure — no React, no React Native — so the transitions that
 * matter can be tested without a renderer, the same split `mediaFrame.ts` and
 * `viewability.ts` use.
 */

/** A rectangle in the app root's coordinate space, in points. */
export type MediaFrame = {
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
};

/**
 * Where the single player's surface is being shown.
 *
 * `inline` is a hole in a screen — a feed card, a carousel page. `fullscreen` is
 * the media viewer's theatre. It decides the surface's fit (`cover` inline,
 * `contain` full screen) as well as its geometry.
 */
export type MediaPlaybackMode = 'inline' | 'fullscreen';

/** The pixel size a clip's metadata reports. */
export type MediaNaturalSize = {
  readonly width: number;
  readonly height: number;
};

/** Everything the engine knows about the one clip it is playing. */
export type MediaPlayerState = {
  /** The identity of the active media. A URL, because that is what every surface shares. */
  readonly activeMediaId: string | null;
  /** The address the player loads (the local cache proxy is applied by the host). */
  readonly uri: string | null;
  readonly mode: MediaPlaybackMode;
  readonly playing: boolean;
  readonly muted: boolean;
  readonly loop: boolean;
  /** Where playback has reached, in seconds. */
  readonly currentTime: number;
  /** The clip's length in seconds, once its metadata has loaded. */
  readonly duration: number;
  /**
   * Where each media was last left, in seconds, keyed by that media's identity.
   *
   * This is the ONE piece of playback that survives a clip change. `currentTime`
   * above describes the clip on screen right now, and a new clip resets it to
   * zero — which is correct for the picture, but wrong for the *media*: scrolling
   * from a clip at 0:05 to another clip must not make the second one open at
   * 0:05, and scrolling back must not forget the first. The positions are kept
   * per media so each surface can ask for its own (`savedPositionFor`) and the
   * answer can never leak between cards (KNOT-051).
   */
  readonly savedPositions: Readonly<Record<string, number>>;
  /** The rectangle the surface occupies, or null while no surface is showing. */
  readonly frame: MediaFrame | null;
  /** True once the player has read the clip's metadata. */
  readonly ready: boolean;
  /**
   * True once the player has drawn a frame. A progress tick is the only proof of
   * that: `onReadyForDisplay` fires on Android as soon as the player is prepared,
   * before the decoder has produced a picture (KNOT-048-fix).
   */
  readonly frameVisible: boolean;
  /** The clip's natural pixel size, reported by its metadata. */
  readonly naturalSize: MediaNaturalSize | null;
};

/** What a surface asks for when it wants the player. */
export type PlayMediaRequest = {
  /** The media's identity. The same clip keeps playing when this matches. */
  readonly id: string;
  /** The address to load. */
  readonly uri: string;
  /** Defaults to `inline`. */
  readonly mode?: MediaPlaybackMode;
  /**
   * Where to start, in seconds. Applied only when it is meaningfully different
   * from the position already held — a viewer opened on the playing clip passes
   * its current position, and that must not rewind it.
   */
  readonly startTime?: number;
  /** Defaults to whatever the engine already holds. */
  readonly muted?: boolean;
  /** Defaults to the existing frame, or null for a new clip. */
  readonly frame?: MediaFrame | null;
  /** Defaults to true: both the feed and the viewer loop a clip. */
  readonly loop?: boolean;
};

export type MediaPlayerAction =
  | { readonly type: 'play'; readonly request: PlayMediaRequest }
  | { readonly type: 'pause'; readonly id?: string }
  | { readonly type: 'resume'; readonly id?: string }
  | { readonly type: 'togglePlayback'; readonly id?: string }
  | { readonly type: 'progress'; readonly id?: string; readonly currentTime: number }
  | { readonly type: 'loaded'; readonly id?: string; readonly duration: number }
  | { readonly type: 'naturalSize'; readonly id?: string; readonly size: MediaNaturalSize }
  | { readonly type: 'seek'; readonly id?: string; readonly currentTime: number }
  | { readonly type: 'frame'; readonly id?: string; readonly frame: MediaFrame | null }
  | { readonly type: 'mode'; readonly id?: string; readonly mode: MediaPlaybackMode }
  | { readonly type: 'muted'; readonly muted: boolean }
  /** Hides the surface without stopping playback (a pager showing a photo). */
  | { readonly type: 'clearSurface'; readonly id?: string }
  /** Leaves the theatre: stops the clip and hands the surface back to the screens. */
  | { readonly type: 'release' }
  | { readonly type: 'reset' };

/** The engine before any surface has asked for a clip. */
export const initialMediaPlayerState: MediaPlayerState = {
  activeMediaId: null,
  uri: null,
  mode: 'inline',
  playing: false,
  muted: true,
  loop: true,
  currentTime: 0,
  duration: 0,
  savedPositions: {},
  frame: null,
  ready: false,
  frameVisible: false,
  naturalSize: null,
};

/**
 * How far a requested `startTime` may be from the held position before it is
 * treated as a seek.
 *
 * A viewer passes the position it read a moment ago, and a render can lag that
 * read by a few hundred milliseconds of playback; seeking on that would be an
 * audible stutter for no gain.
 */
export const CONTINUE_TOLERANCE_SECONDS = 0.5;

/** A finite, non-negative number, or null. */
function finitePosition(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null;
}

/** A finite, strictly positive number, or null. */
function positive(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? value : null;
}

/** Whether an action targeting `id` belongs to the active media. */
function targetsActive(state: MediaPlayerState, id?: string): boolean {
  return id === undefined || id === state.activeMediaId;
}

/** A media identity as it is keyed, or '' when it cannot name anything. */
function mediaKey(mediaId: unknown): string {
  return typeof mediaId === 'string' ? mediaId.trim() : '';
}

/**
 * Records `position` as where `id` was left.
 *
 * Returns the map it was given when nothing changes, so a repeated report costs
 * no new object and a progress tick that lands on the same second cannot churn a
 * subscription. A missing identity (no `id` and no active media) records nothing:
 * a position with no media to belong to is exactly the global playhead this
 * task removes (KNOT-051).
 */
function withSavedPosition(
  positions: Readonly<Record<string, number>>,
  id: string | null,
  position: number | null,
): Readonly<Record<string, number>> {
  if (id === null || position === null || positions[id] === position) {
    return positions;
  }
  return { ...positions, [id]: position };
}

/**
 * Where `mediaId` was last left, in seconds — or 0 when it has never played.
 *
 * A surface asks this about ITS OWN media, never about "the playhead": that is
 * what keeps the position of one card from bleeding into the next (KNOT-051).
 */
export function savedPositionFor(state: MediaPlayerState, mediaId: string): number {
  const key = mediaKey(mediaId);
  if (key === '') {
    return 0;
  }
  const saved = state.savedPositions[key];
  return finitePosition(saved) ?? 0;
}

/**
 * The position a `play` for a NEW clip must be seeked to after it loads, or null
 * when no seek is needed.
 *
 * The surface mounts a fresh native player when the media changes, so the
 * position has to be applied once its metadata is ready. A request to continue
 * the clip that is already loaded needs no seek at all — the native player never
 * lost it.
 */
export function seekTargetFor(state: MediaPlayerState, request: PlayMediaRequest): number | null {
  const start = finitePosition(request.startTime);
  if (start === null) {
    return null;
  }

  const sameClip = state.activeMediaId === request.id;
  if (sameClip) {
    return Math.abs(start - state.currentTime) > CONTINUE_TOLERANCE_SECONDS ? start : null;
  }

  return start > 0 ? start : null;
}

/**
 * The engine's transitions.
 *
 * Every transition that names a media is ignored when it names a different one,
 * so a late event from a player that has already been replaced can never move the
 * clip that replaced it.
 */
export function mediaPlayerReducer(
  state: MediaPlayerState,
  action: MediaPlayerAction,
): MediaPlayerState {
  switch (action.type) {
    case 'play': {
      const id = typeof action.request.id === 'string' ? action.request.id.trim() : '';
      const uri = typeof action.request.uri === 'string' ? action.request.uri.trim() : '';
      if (id === '' || uri === '') {
        return state;
      }

      // The clip already on screen: carry on. Only the presentation changes, which
      // is what makes the viewer open at the second the feed had reached.
      if (state.activeMediaId === id) {
        const requested = finitePosition(action.request.startTime);
        const currentTime =
          requested !== null && Math.abs(requested - state.currentTime) > CONTINUE_TOLERANCE_SECONDS
            ? requested
            : state.currentTime;

        return {
          ...state,
          uri,
          playing: true,
          mode: action.request.mode ?? state.mode,
          muted: action.request.muted ?? state.muted,
          loop: action.request.loop ?? state.loop,
          currentTime,
          savedPositions: withSavedPosition(state.savedPositions, id, currentTime),
          frame: action.request.frame === undefined ? state.frame : action.request.frame,
        };
      }

      const start = finitePosition(action.request.startTime);

      // A different clip: a new payload, a new position, and nothing carried over
      // that could describe the previous one. Where each MEDIA was left is the one
      // exception: it is keyed by the media itself, so it survives the change and
      // the clip that follows cannot inherit the previous clip's second (KNOT-051).
      return {
        ...state,
        activeMediaId: id,
        uri,
        mode: action.request.mode ?? 'inline',
        playing: true,
        muted: action.request.muted ?? state.muted,
        loop: action.request.loop ?? true,
        currentTime: start ?? 0,
        duration: 0,
        savedPositions: withSavedPosition(state.savedPositions, id, start),
        frame: action.request.frame ?? null,
        ready: false,
        frameVisible: false,
        naturalSize: null,
      };
    }

    case 'pause': {
      if (!targetsActive(state, action.id) || !state.playing) {
        return state;
      }
      // Pausing keeps the surface where it is — a viewer left open on a paused
      // clip still shows the picture. Releasing it is a separate intent, so the
      // feed cannot reclaim the player behind the viewer's modal (KNOT-051).
      return { ...state, playing: false };
    }

    case 'resume': {
      if (!targetsActive(state, action.id) || state.playing) {
        return state;
      }
      return { ...state, playing: true };
    }

    case 'togglePlayback': {
      if (!targetsActive(state, action.id)) {
        return state;
      }
      return { ...state, playing: !state.playing };
    }

    case 'progress': {
      const position = finitePosition(action.currentTime);
      if (position === null) {
        return state;
      }

      // Where a clip has reached is a fact about THAT media, so it is recorded even
      // when the report arrives after another clip has taken the surface: a card
      // being scrolled away must not lose the second it had reached (KNOT-051).
      const saved = withSavedPosition(
        state.savedPositions,
        action.id ?? state.activeMediaId,
        position,
      );

      if (!targetsActive(state, action.id)) {
        // A late report is allowed to record its own media's position, but never to
        // move the clip that replaced it.
        return saved === state.savedPositions ? state : { ...state, savedPositions: saved };
      }

      return { ...state, currentTime: position, frameVisible: true, savedPositions: saved };
    }

    case 'loaded': {
      const duration = positive(action.duration);
      if (!targetsActive(state, action.id) || duration === null) {
        return state;
      }
      return { ...state, duration, ready: true };
    }

    case 'naturalSize': {
      const width = positive(action.size.width);
      const height = positive(action.size.height);
      if (!targetsActive(state, action.id) || width === null || height === null) {
        return state;
      }
      return { ...state, naturalSize: { width, height } };
    }

    case 'seek': {
      const position = finitePosition(action.currentTime);
      if (!targetsActive(state, action.id) || position === null) {
        return state;
      }
      // A scrub is a playhead too: it belongs to this media, so a viewer left on a
      // scrubbed second hands that second back to the card (KNOT-051).
      return {
        ...state,
        currentTime: position,
        savedPositions: withSavedPosition(
          state.savedPositions,
          action.id ?? state.activeMediaId,
          position,
        ),
      };
    }

    case 'frame': {
      if (!targetsActive(state, action.id)) {
        return state;
      }
      return { ...state, frame: action.frame };
    }

    case 'mode': {
      if (!targetsActive(state, action.id)) {
        return state;
      }
      return { ...state, mode: action.mode };
    }

    case 'muted':
      return { ...state, muted: action.muted };

    case 'clearSurface': {
      if (!targetsActive(state, action.id)) {
        return state;
      }
      if (state.frame === null && !state.frameVisible) {
        return state;
      }
      // Nothing is drawn once the surface is hidden, so nothing is visible either.
      // A card reads this to decide whether its poster still covers the picture:
      // the player is unmounted whenever a card scrolls or another card takes the
      // surface, and the still has to be back in place by then, or the row would
      // paint an empty hole where its video was (KNOT-049).
      return { ...state, frame: null, frameVisible: false };
    }

    case 'release': {
      // Nothing is playing any more, and the presentation goes back to the
      // screens: the viewer's full-screen frame is no longer wanted, so the feed
      // can attach its own and resume at the position held here.
      if (state.activeMediaId === null) {
        return state;
      }
      return { ...state, playing: false, mode: 'inline', frame: null };
    }

    case 'reset':
      return initialMediaPlayerState;

    default:
      return state;
  }
}

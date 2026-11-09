import {
  CONTINUE_TOLERANCE_SECONDS,
  initialMediaPlayerState,
  mediaPlayerReducer,
  savedPositionFor,
  seekTargetFor,
  type MediaPlayerState,
  type PlayMediaRequest,
} from '../mediaPlayerState';

/**
 * The unified engine's transitions (KNOT-051).
 *
 * The behaviour that matters most is "Continue Playing": asking for the clip that
 * is already active must carry the position over, while asking for a different
 * clip must start a new payload. Everything else — pause, progress, seek, the
 * surface rectangle — is a plain state update, and each is covered here so a
 * regression cannot hide behind a renderer-less suite.
 */
const FIRST: PlayMediaRequest = {
  id: 'media-1',
  uri: 'http://api/stories/1/media/1/content',
};

const SECOND: PlayMediaRequest = {
  id: 'media-2',
  uri: 'http://api/stories/1/media/2/content',
};

/** The state after `play` requests, in order. */
function played(...requests: PlayMediaRequest[]): MediaPlayerState {
  return requests.reduce<MediaPlayerState>(
    (state, request) => mediaPlayerReducer(state, { type: 'play', request }),
    initialMediaPlayerState,
  );
}

describe('mediaPlayerReducer play', () => {
  it('starts a new clip at zero, playing, inline', () => {
    const state = played(FIRST);

    expect(state.activeMediaId).toBe('media-1');
    expect(state.uri).toBe(FIRST.uri);
    expect(state.mode).toBe('inline');
    expect(state.playing).toBe(true);
    expect(state.currentTime).toBe(0);
    expect(state.duration).toBe(0);
    expect(state.ready).toBe(false);
  });

  it('honours a start time for a clip it is not already holding', () => {
    expect(played({ ...FIRST, startTime: 19 }).currentTime).toBe(19);
  });

  it('carries the position over when the SAME clip is asked for again', () => {
    const playing = mediaPlayerReducer(played(FIRST), {
      type: 'progress',
      id: 'media-1',
      currentTime: 19,
    });

    // This is the viewer opening on the feed's playing video: the clip must not
    // rewind, so the provider only changes where the picture is shown.
    const opened = mediaPlayerReducer(playing, {
      type: 'play',
      request: { ...FIRST, mode: 'fullscreen', startTime: 19 },
    });

    expect(opened.currentTime).toBe(19);
    expect(opened.mode).toBe('fullscreen');
    expect(opened.duration).toBe(0);
  });

  it('seeks when the same clip is asked for at a meaningfully different time', () => {
    const playing = mediaPlayerReducer(played(FIRST), {
      type: 'progress',
      id: 'media-1',
      currentTime: 4,
    });
    const seeked = mediaPlayerReducer(playing, {
      type: 'play',
      request: { ...FIRST, startTime: 19 },
    });

    expect(seeked.currentTime).toBe(19);
  });

  it('ignores a sub-tolerance difference, which is just playback lag', () => {
    const playing = mediaPlayerReducer(played(FIRST), {
      type: 'progress',
      id: 'media-1',
      currentTime: 19,
    });
    const replayed = mediaPlayerReducer(playing, {
      type: 'play',
      request: { ...FIRST, startTime: 19 + CONTINUE_TOLERANCE_SECONDS / 2 },
    });

    expect(replayed.currentTime).toBe(19);
  });

  it('resets every fact about the previous clip when the media changes', () => {
    const first = mediaPlayerReducer(
      mediaPlayerReducer(
        mediaPlayerReducer(played(FIRST), { type: 'loaded', id: 'media-1', duration: 45 }),
        { type: 'progress', id: 'media-1', currentTime: 19 },
      ),
      { type: 'naturalSize', id: 'media-1', size: { width: 1080, height: 1920 } },
    );

    const second = mediaPlayerReducer(first, { type: 'play', request: SECOND });

    expect(second.activeMediaId).toBe('media-2');
    expect(second.currentTime).toBe(0);
    expect(second.duration).toBe(0);
    expect(second.ready).toBe(false);
    expect(second.frameVisible).toBe(false);
    expect(second.naturalSize).toBeNull();
  });

  it('rejects a request with no identity or no address', () => {
    expect(played({ ...FIRST, id: '  ' })).toBe(initialMediaPlayerState);
    expect(played({ ...FIRST, uri: '  ' })).toBe(initialMediaPlayerState);
  });
});

describe('mediaPlayerReducer pause and resume', () => {
  it('pauses the active clip without moving its surface', () => {
    const playing = mediaPlayerReducer(played(FIRST), {
      type: 'frame',
      id: 'media-1',
      frame: { x: 0, y: 100, width: 390, height: 220 },
    });
    const paused = mediaPlayerReducer(playing, { type: 'pause', id: 'media-1' });

    expect(paused.playing).toBe(false);
    // A paused viewer still shows the picture, so the frame stays.
    expect(paused.frame).toEqual({ x: 0, y: 100, width: 390, height: 220 });
  });

  it('ignores a pause aimed at a clip that is not active', () => {
    const playing = played(FIRST);

    expect(mediaPlayerReducer(playing, { type: 'pause', id: 'media-2' })).toBe(playing);
  });

  it('resumes without reloading', () => {
    const paused = mediaPlayerReducer(played(FIRST), { type: 'pause' });
    const resumed = mediaPlayerReducer(paused, { type: 'resume' });

    expect(resumed.playing).toBe(true);
    expect(resumed.uri).toBe(FIRST.uri);
  });

  it('toggles play/pause', () => {
    const paused = mediaPlayerReducer(played(FIRST), { type: 'togglePlayback', id: 'media-1' });
    const resumed = mediaPlayerReducer(paused, { type: 'togglePlayback', id: 'media-1' });

    expect(paused.playing).toBe(false);
    expect(resumed.playing).toBe(true);
  });
});

describe('mediaPlayerReducer playback state', () => {
  it('advances the playhead and records that a frame was drawn', () => {
    const state = mediaPlayerReducer(played(FIRST), {
      type: 'progress',
      id: 'media-1',
      currentTime: 12.5,
    });

    expect(state.currentTime).toBe(12.5);
    // A progress tick is the only proof a picture exists (KNOT-048-fix).
    expect(state.frameVisible).toBe(true);
  });

  it('ignores a progress report that is not a real position', () => {
    const playing = played(FIRST);

    for (const currentTime of [Number.NaN, -1, Number.POSITIVE_INFINITY]) {
      expect(mediaPlayerReducer(playing, { type: 'progress', currentTime })).toBe(playing);
    }
  });

  it('never lets a late report move the clip that replaced it', () => {
    const playing = played(FIRST);
    const late = mediaPlayerReducer(playing, {
      type: 'progress',
      id: 'media-2',
      currentTime: 3,
    });

    // The picture on screen is untouched...
    expect(late.activeMediaId).toBe('media-1');
    expect(late.currentTime).toBe(0);
    expect(late.frameVisible).toBe(false);
    // ...but the report is still a fact about the media it named, and a card being
    // scrolled away must not lose the second it had reached (KNOT-051).
    expect(savedPositionFor(late, 'media-2')).toBe(3);
  });

  it('records the duration once the metadata loads', () => {
    const state = mediaPlayerReducer(played(FIRST), {
      type: 'loaded',
      id: 'media-1',
      duration: 45,
    });

    expect(state.duration).toBe(45);
    expect(state.ready).toBe(true);
  });

  it('ignores a zero or broken duration', () => {
    const playing = played(FIRST);

    expect(mediaPlayerReducer(playing, { type: 'loaded', duration: 0 })).toBe(playing);
    expect(mediaPlayerReducer(playing, { type: 'loaded', duration: Number.NaN })).toBe(playing);
  });

  it('records the natural size the metadata reports', () => {
    const state = mediaPlayerReducer(played(FIRST), {
      type: 'naturalSize',
      id: 'media-1',
      size: { width: 300, height: 700 },
    });

    expect(state.naturalSize).toEqual({ width: 300, height: 700 });
  });

  it('moves the playhead on a seek', () => {
    const state = mediaPlayerReducer(played(FIRST), {
      type: 'seek',
      id: 'media-1',
      currentTime: 30,
    });

    expect(state.currentTime).toBe(30);
  });
});

describe('mediaPlayerReducer surface', () => {
  it('moves the surface to the reported frame', () => {
    const frame = { x: 16, y: 240, width: 358, height: 201 };
    const state = mediaPlayerReducer(played(FIRST), { type: 'frame', id: 'media-1', frame });

    expect(state.frame).toEqual(frame);
  });

  it('hides the surface without stopping playback', () => {
    const playing = mediaPlayerReducer(
      mediaPlayerReducer(played(FIRST), {
        type: 'frame',
        id: 'media-1',
        frame: { x: 0, y: 0, width: 390, height: 844 },
      }),
      { type: 'progress', id: 'media-1', currentTime: 4 },
    );
    const hidden = mediaPlayerReducer(playing, { type: 'clearSurface', id: 'media-1' });

    expect(hidden.frame).toBeNull();
    expect(hidden.playing).toBe(true);
    // Nothing is drawn, so nothing is visible either: the card's still has to cover
    // the hole again the moment the player is unmounted (KNOT-049).
    expect(hidden.frameVisible).toBe(false);
  });

  it('releases the theatre: stops the clip and hands the surface back inline', () => {
    const playing = mediaPlayerReducer(
      mediaPlayerReducer(played(FIRST), {
        type: 'play',
        request: { ...FIRST, mode: 'fullscreen', frame: { x: 0, y: 0, width: 390, height: 844 } },
      }),
      { type: 'progress', id: 'media-1', currentTime: 19 },
    );

    const released = mediaPlayerReducer(playing, { type: 'release' });

    expect(released.playing).toBe(false);
    expect(released.mode).toBe('inline');
    expect(released.frame).toBeNull();
    // The position is kept, so the feed resumes where the viewer left off.
    expect(released.currentTime).toBe(19);
  });

  it('leaves the position untouched when release runs with nothing playing', () => {
    expect(mediaPlayerReducer(initialMediaPlayerState, { type: 'release' })).toBe(
      initialMediaPlayerState,
    );
  });

  it('holds one mute setting for the whole app', () => {
    expect(mediaPlayerReducer(played(FIRST), { type: 'muted', muted: false }).muted).toBe(false);
  });
});

/**
 * A playhead belongs to a media, not to the engine (KNOT-051).
 *
 * The bug this covers: the engine held one `currentTime`, so scrolling away from a
 * clip at 0:05 seated the next card at 0:05. Every position is now keyed by the
 * media it describes — a clip change keeps the map while resetting the picture.
 */
describe('per-media saved positions', () => {
  it('does not share a playhead between two videos', () => {
    const first = mediaPlayerReducer(played(FIRST), {
      type: 'progress',
      id: 'media-1',
      currentTime: 5,
    });
    const second = mediaPlayerReducer(first, { type: 'play', request: SECOND });

    // The second card starts from zero, however far the first one had reached...
    expect(second.currentTime).toBe(0);
    expect(savedPositionFor(second, SECOND.id)).toBe(0);
    // ...and the first card keeps its own second for when it comes back.
    expect(savedPositionFor(second, FIRST.id)).toBe(5);
  });

  it('resumes a video at the position saved for that media', () => {
    const watched = mediaPlayerReducer(played(FIRST), {
      type: 'progress',
      id: 'media-1',
      currentTime: 19,
    });
    // Scroll away to another clip, then back to the first one.
    const elsewhere = mediaPlayerReducer(watched, { type: 'play', request: SECOND });
    const returned = mediaPlayerReducer(elsewhere, { type: 'play', request: FIRST });

    // The card asks this answer before its player mounts, so it opens at 0:19.
    expect(savedPositionFor(returned, FIRST.id)).toBe(19);
    // The picture itself still starts clean: the surface seeks, it never inherits.
    expect(returned.currentTime).toBe(0);
  });

  it('answers zero for a media nobody has played', () => {
    expect(savedPositionFor(initialMediaPlayerState, FIRST.id)).toBe(0);
    expect(savedPositionFor(played(FIRST), 'media-999')).toBe(0);
    expect(savedPositionFor(played(FIRST), '   ')).toBe(0);
  });

  it('keeps a scrub as a position for that media', () => {
    const scrubbed = mediaPlayerReducer(played(FIRST), {
      type: 'seek',
      id: 'media-1',
      currentTime: 30,
    });

    expect(savedPositionFor(scrubbed, FIRST.id)).toBe(30);
  });
});

describe('seekTargetFor', () => {
  it('asks for no seek when the same clip is already where it should be', () => {
    const playing = mediaPlayerReducer(played(FIRST), {
      type: 'progress',
      id: 'media-1',
      currentTime: 19,
    });

    expect(seekTargetFor(playing, { ...FIRST, startTime: 19 })).toBeNull();
  });

  it('asks for a seek when the same clip must jump', () => {
    expect(seekTargetFor(played(FIRST), { ...FIRST, startTime: 19 })).toBe(19);
  });

  it('asks for a seek on a new clip only when the start is positive', () => {
    expect(seekTargetFor(played(FIRST), { ...SECOND, startTime: 0 })).toBeNull();
    expect(seekTargetFor(played(FIRST), { ...SECOND, startTime: 7 })).toBe(7);
  });
});

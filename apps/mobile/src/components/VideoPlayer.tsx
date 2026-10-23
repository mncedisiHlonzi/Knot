import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Animated,
  Easing,
  PanResponder,
  Pressable,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { Pause, Play, SpeakerHigh, SpeakerSlash } from 'phosphor-react-native';
import Video, { type OnLoadData } from 'react-native-video';

import {
  motion,
  radius,
  spacing,
  tabularNums,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../theme';
import type { ThemeColors } from '../theme';
import { scrubSeconds, viewerProgress, viewerTimeLabel } from './mediaViewer';

/**
 * Whether a video starts muted.
 *
 * The app's rule for a video that starts itself is that it must never make a
 * sound (FeedVideo). A viewer opened on purpose is a different context — the
 * person chose to watch — but the tap that opened it did not ask for audio, so
 * it starts muted and the unmute control is the right-hand toggle of the control
 * bar (KNOT-020k). Flip this one constant to start with sound.
 */
const START_MUTED = true;

/** The pause disc drawn mid-picture while the video is not running. */
const CENTRE_DISC_SIZE = 68;
const CENTRE_ICON_SIZE = 30;

/** The control bar's own play/pause target. */
const PLAY_BUTTON_SIZE = 40;
const PLAY_ICON_SIZE = 22;

/** The mute control's disc and glyph. */
const MUTE_DISC_SIZE = 36;
const MUTE_ICON_SIZE = 18;

/** The scrub track at rest, and while a finger is on it. */
const TRACK_HEIGHT = 3;
const TRACK_ACTIVE_HEIGHT = 5;
const THUMB_ACTIVE_SIZE = 16;

/** The touch band around the scrub track, so a 3pt line is still grabbable. */
const TRACK_TOUCH_HEIGHT = 32;

/** The control bar's inset from the page's sides. */
const BAR_INSET = spacing.md;

type VideoPlayerProps = {
  /** The resolved, absolute URL of the video. */
  readonly uri: string;
  /**
   * The page's width, in points.
   *
   * A page inside a horizontal list cannot size itself: the list gives each cell
   * a row container with no width, so `flex: 1` resolves to zero and the native
   * video surface is never drawn. The pager therefore measures the page and
   * passes the size in, exactly as `ZoomableImage` does (KNOT-020k-fix).
   */
  readonly width: number;
  /** The page's height, in points. See {@link width}. */
  readonly height: number;
  /**
   * True while this page is the one on screen. It is the only input that starts
   * or stops playback, so a gallery of videos plays exactly one at a time.
   */
  readonly isActive: boolean;
  /** The duration the payload knows, in milliseconds, to seed the total time. */
  readonly durationMs?: number | null;
  /**
   * Whether the viewer's chrome — its top bar and this control bar — is showing.
   *
   * The viewer owns this, because a tap on the picture must hide both bars at
   * once; the player only draws and fades what the viewer decides
   * (KNOT-020k-fix2).
   */
  readonly chromeVisible: boolean;
  /** Called when the picture itself is tapped, to show or hide that chrome. */
  readonly onToggleChrome: () => void;
  /**
   * How far the control bar floats above the page's bottom edge, in points. The
   * viewer passes the context overlay's measured height so the bar is never
   * covered by it (KNOT-020k-fix2).
   */
  readonly bottomInset?: number;
};

/**
 * The media viewer's video page: a full-bleed player with its own controls.
 *
 * The platform controls are deliberately unused — they are a light, stock sheet
 * that breaks the theatre — so this draws a modern floating bar: a thin scrub
 * line that swells into a grabbable handle while a finger is on it, and beneath
 * it the play/pause control, the running time (`0:12 / 0:45`) and the mute
 * toggle. While the picture is still, a single pause disc sits over it; a
 * playing video is left clean (KNOT-020k-fix2).
 *
 * Chrome visibility belongs to the viewer: tapping the picture calls
 * `onToggleChrome`, so the top bar and this bar hide and return together, and
 * the tap never closes the viewer — that is the gallery's swipe-down gesture.
 *
 * The chrome fades with `Animated` on the native driver. The scrub bar is a
 * `PanResponder`: a touch anywhere on its band seeks there on release, and a drag
 * previews the target time before committing — so the bar follows the finger
 * without flooding the player with seeks it cannot serve.
 */
export default function VideoPlayer({
  uri,
  width,
  height,
  isActive,
  durationMs,
  chromeVisible,
  onToggleChrome,
  bottomInset = spacing.xl,
}: VideoPlayerProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const videoRef = useRef<React.ComponentRef<typeof Video>>(null);

  const [userPaused, setUserPaused] = useState(false);
  const [ended, setEnded] = useState(false);
  const [muted, setMuted] = useState(START_MUTED);
  const [loaded, setLoaded] = useState(false);
  const [duration, setDuration] = useState(() =>
    typeof durationMs === 'number' && Number.isFinite(durationMs) && durationMs > 0
      ? durationMs / 1000
      : 0,
  );
  const [current, setCurrent] = useState(0);

  // Whether a finger is on the scrub band: the line thickens and grows a handle
  // while it is, so the bar answers the touch before the seek lands
  // (KNOT-020k-fix2).
  const [dragging, setDragging] = useState(false);

  // The chrome's fade runs on the native driver; its visibility belongs to the
  // viewer, so one tap can hide the top bar and this bar together.
  const chromeOpacity = useRef(new Animated.Value(1)).current;

  // Refs mirror the values the responders read, so the gesture callbacks never
  // close over a stale render.
  const durationRef = useRef(duration);
  const trackWidth = useRef(0);
  const seeking = useRef(false);
  const previewRef = useRef<number | null>(null);
  const [preview, setPreview] = useState<number | null>(null);

  useEffect(() => {
    durationRef.current = duration;
  }, [duration]);

  useEffect(() => {
    Animated.timing(chromeOpacity, {
      toValue: chromeVisible ? 1 : 0,
      duration: motion.durations.small,
      easing: Easing.bezier(...motion.easing.standard),
      useNativeDriver: true,
    }).start();
  }, [chromeOpacity, chromeVisible]);

  const seek = useCallback((seconds: number): void => {
    videoRef.current?.seek(seconds);
    setCurrent(seconds);
  }, []);

  const togglePlayback = useCallback((): void => {
    if (ended) {
      // Replay from the start rather than leaving a finished video paused at its
      // last frame.
      seek(0);
      setEnded(false);
      setUserPaused(false);
      return;
    }
    setUserPaused((paused) => !paused);
  }, [ended, seek]);

  const handleLoad = useCallback((data: OnLoadData): void => {
    setLoaded(true);
    if (typeof data.duration === 'number' && Number.isFinite(data.duration) && data.duration > 0) {
      setDuration(data.duration);
    }
  }, []);

  /** Previews the time under a touch on the scrub band, without seeking yet. */
  const previewAt = useCallback((x: number): void => {
    const width = trackWidth.current;
    if (width <= 0 || durationRef.current <= 0) {
      return;
    }
    const seconds = scrubSeconds(x / width, durationRef.current);
    previewRef.current = seconds;
    setPreview(seconds);
  }, []);

  const scrubResponder = useMemo(
    () =>
      PanResponder.create({
        onStartShouldSetPanResponder: () => true,
        onMoveShouldSetPanResponder: () => true,
        // A scrub in progress must not be taken over by the gallery's dismiss
        // gesture.
        onPanResponderTerminationRequest: () => false,
        onPanResponderGrant: (event) => {
          seeking.current = true;
          setDragging(true);
          previewAt(event.nativeEvent.locationX);
        },
        onPanResponderMove: (event) => {
          previewAt(event.nativeEvent.locationX);
        },
        onPanResponderRelease: () => {
          seeking.current = false;
          setDragging(false);
          const target = previewRef.current;
          previewRef.current = null;
          setPreview(null);
          if (target !== null) {
            seek(target);
          }
        },
        onPanResponderTerminate: () => {
          seeking.current = false;
          setDragging(false);
          previewRef.current = null;
          setPreview(null);
        },
      }),
    [previewAt, seek],
  );

  const shownTime = preview ?? current;
  const progress = viewerProgress(shownTime, duration);
  const playing = isActive && !userPaused && !ended;

  return (
    <View style={[styles.container, { height, width }]}>
      <Video
        ref={videoRef}
        source={{ uri }}
        style={styles.video}
        paused={!playing}
        muted={muted}
        repeat={false}
        resizeMode="contain"
        progressUpdateInterval={250}
        onLoad={handleLoad}
        onProgress={(data) => {
          if (!seeking.current) {
            setCurrent(data.currentTime);
          }
        }}
        onEnd={() => {
          setEnded(true);
          setCurrent(durationRef.current);
        }}
      />

      {loaded ? null : <ActivityIndicator style={styles.loader} color={colors.text.onMedia} />}

      {/* The tap layer spans the whole picture and sits under the controls, so a
          tap on the video toggles the viewer's chrome while a tap on a control
          still reaches that control (KNOT-020k-fix2). */}
      <Pressable
        style={StyleSheet.absoluteFill}
        onPress={onToggleChrome}
        accessibilityRole="button"
        accessibilityLabel={chromeVisible ? 'Hide video controls' : 'Show video controls'}
      />

      <Animated.View
        style={[styles.overlay, { opacity: chromeOpacity }]}
        pointerEvents={chromeVisible ? 'box-none' : 'none'}
      >
        {/* The pause disc is drawn only while the picture is still: a playing
            video is a clean image with one quiet bar under it. */}
        {playing ? null : (
          <Pressable
            style={[
              styles.centreDisc,
              // Centred in the picture the sheet leaves visible, not in the whole
              // window, so the disc never hides behind the sheet.
              { top: (height - bottomInset) / 2 - CENTRE_DISC_SIZE / 2 },
            ]}
            onPress={togglePlayback}
            accessibilityRole="button"
            accessibilityLabel={ended ? 'Replay video' : 'Play video'}
          >
            <Play color={colors.text.onMedia} size={CENTRE_ICON_SIZE} weight="fill" />
          </Pressable>
        )}

        <View style={[styles.controlBar, { bottom: bottomInset }]}>
          <View
            style={styles.trackTouch}
            onLayout={(event) => {
              trackWidth.current = event.nativeEvent.layout.width;
            }}
            {...scrubResponder.panHandlers}
            accessibilityRole="adjustable"
            accessibilityLabel="Video position"
            accessibilityValue={{ min: 0, max: Math.round(duration) }}
          >
            <View style={[styles.track, dragging ? styles.trackActive : null]}>
              <View style={[styles.trackFill, { width: `${progress * 100}%` }]} />
              {dragging ? (
                <View
                  style={[styles.thumbActive, { left: `${progress * 100}%` }]}
                  pointerEvents="none"
                />
              ) : null}
            </View>
          </View>

          <View style={styles.buttonRow}>
            <Pressable
              style={styles.playButton}
              onPress={togglePlayback}
              accessibilityRole="button"
              accessibilityLabel={playing ? 'Pause video' : 'Play video'}
            >
              {playing ? (
                <Pause color={colors.text.onMedia} size={PLAY_ICON_SIZE} weight="fill" />
              ) : (
                <Play color={colors.text.onMedia} size={PLAY_ICON_SIZE} weight="fill" />
              )}
            </Pressable>

            <Text style={[typeScale.label, tabularNums, styles.time]}>
              {`${viewerTimeLabel(shownTime)} / ${viewerTimeLabel(duration)}`}
            </Text>

            <View style={styles.spacer} />

            <Pressable
              style={styles.muteButton}
              onPress={() => setMuted((value) => !value)}
              accessibilityRole="button"
              accessibilityLabel={muted ? 'Unmute video' : 'Mute video'}
            >
              {muted ? (
                <SpeakerSlash color={colors.text.onMedia} size={MUTE_ICON_SIZE} weight="fill" />
              ) : (
                <SpeakerHigh color={colors.text.onMedia} size={MUTE_ICON_SIZE} weight="fill" />
              )}
            </Pressable>
          </View>
        </View>
      </Animated.View>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    buttonRow: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.md,
      marginTop: spacing.xs,
    },
    // The pause disc: a scrim disc behind the glyph, because the frame behind it
    // may be any brightness. Its `top` is supplied inline, from the visible area.
    centreDisc: {
      alignItems: 'center',
      alignSelf: 'center',
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.pill,
      height: CENTRE_DISC_SIZE,
      justifyContent: 'center',
      position: 'absolute',
      width: CENTRE_DISC_SIZE,
    },
    container: {
      backgroundColor: colors.bg.theater,
    },
    // The floating bar: one translucent card that holds the scrub line and, under
    // it, the transport row. It rides above the contextual sheet rather than
    // behind it (KNOT-020k-fix2).
    controlBar: {
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.xl,
      left: BAR_INSET,
      paddingHorizontal: spacing.lg,
      paddingVertical: spacing.sm,
      position: 'absolute',
      right: BAR_INSET,
    },
    loader: {
      ...StyleSheet.absoluteFillObject,
      alignItems: 'center',
      justifyContent: 'center',
    },
    muteButton: {
      alignItems: 'center',
      height: MUTE_DISC_SIZE,
      justifyContent: 'center',
      width: MUTE_DISC_SIZE,
    },
    overlay: {
      ...StyleSheet.absoluteFillObject,
    },
    playButton: {
      alignItems: 'center',
      height: PLAY_BUTTON_SIZE,
      justifyContent: 'center',
      width: PLAY_BUTTON_SIZE,
    },
    spacer: {
      flex: 1,
    },
    // The played portion and the handle are white; the unplayed track is the
    // tertiary ink, which reads against the scrim in both themes.
    thumbActive: {
      backgroundColor: colors.text.onMedia,
      borderRadius: radius.pill,
      height: THUMB_ACTIVE_SIZE,
      marginLeft: -THUMB_ACTIVE_SIZE / 2,
      marginTop: -THUMB_ACTIVE_SIZE / 2,
      position: 'absolute',
      top: '50%',
      width: THUMB_ACTIVE_SIZE,
    },
    time: {
      color: colors.text.onMedia,
    },
    track: {
      backgroundColor: colors.text.tertiary,
      borderRadius: radius.pill,
      height: TRACK_HEIGHT,
      overflow: 'visible',
      width: '100%',
    },
    // The line swells under the finger, so the scrub target is felt as well as
    // seen (KNOT-020k-fix2).
    trackActive: {
      height: TRACK_ACTIVE_HEIGHT,
    },
    trackFill: {
      backgroundColor: colors.text.onMedia,
      borderRadius: radius.pill,
      height: '100%',
    },
    // A thin visual line inside a tall touch band, so the bar is easy to grab
    // without a heavy rule across the picture.
    trackTouch: {
      height: TRACK_TOUCH_HEIGHT,
      justifyContent: 'center',
    },
    video: {
      ...StyleSheet.absoluteFillObject,
    },
  });

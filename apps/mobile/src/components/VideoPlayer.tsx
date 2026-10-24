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
import { SpeakerHigh, SpeakerSlash } from 'phosphor-react-native';
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
 * The viewer is opened deliberately — the person tapped the media to watch it —
 * so its video starts with sound (KNOT-020k-fix4). Mute stays one tap away in the
 * control bar, and the Feed's in-place previews keep their own always-muted rule
 * (`FeedVideo`).
 */
const START_MUTED = false;

/** The mute control's disc and glyph. */
const MUTE_DISC_SIZE = 36;
const MUTE_ICON_SIZE = 18;

/** The scrub track at rest, and while a finger is on it. */
const TRACK_HEIGHT = 3;
const TRACK_ACTIVE_HEIGHT = 5;
const THUMB_ACTIVE_SIZE = 16;

/** The touch band around the scrub track, so a 3pt line is still grabbable. */
const TRACK_TOUCH_HEIGHT = 32;

/** The gap between the scrub line and the transport row. */
const CONTROLS_GAP = spacing.xs;

/** The bar's own bottom padding, and its distance from the page's bottom edge. */
const CONTROLS_PADDING = spacing.sm;
const BAR_BOTTOM = spacing.sm;

/**
 * The transport stack's total height, exported so the viewer can keep the story
 * text clear of it instead of guessing (KNOT-020k-fix4).
 */
export const VIDEO_CONTROLS_HEIGHT =
  TRACK_TOUCH_HEIGHT + CONTROLS_GAP + MUTE_DISC_SIZE + CONTROLS_PADDING + BAR_BOTTOM;

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
   * Whether the viewer's chrome — its top bar and this transport bar — is showing.
   *
   * The viewer owns this, because a tap on the picture must hide both bars at
   * once; the player only draws and fades what the viewer decides
   * (KNOT-020k-fix2).
   */
  readonly chromeVisible: boolean;
  /**
   * Called when the picture itself is tapped. The viewer hides or shows its chrome
   * from this, and the player toggles playback with it — the picture is the
   * play/pause control (KNOT-020k-fix4).
   */
  readonly onToggleChrome: () => void;
};

/**
 * The media viewer's video page: a full-bleed player with its own transport.
 *
 * The platform controls are deliberately unused — they are a light, stock sheet
 * that breaks the theatre — and so is any persistent button: the picture itself is
 * the play/pause control, and a tap on it also hides or shows the chrome. What is
 * drawn is only a transparent strip along the bottom: a thin scrub line that
 * swells into a grabbable handle under the finger, the running time
 * (`0:12 / 0:45`), and the mute toggle. Nothing is filled behind it, so the frame
 * runs behind the controls and only the viewer's gradient scrim dims them
 * (KNOT-020k-fix4).
 *
 * Chrome visibility belongs to the viewer: tapping the picture calls
 * `onToggleChrome` and `togglePlayback` together, so the top bar, the transport
 * and the story overlay hide and return as one, and the tap never closes the
 * viewer — that is the gallery's swipe-down gesture.
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

      {/* The picture is the control: a tap shows or hides the chrome and starts or
          pauses playback (KNOT-020k-fix4). The controls sit above this layer, so
          their own taps still reach them. */}
      <Pressable
        style={StyleSheet.absoluteFill}
        onPress={() => {
          onToggleChrome();
          togglePlayback();
        }}
        accessibilityRole="button"
        accessibilityLabel={chromeVisible ? 'Hide video controls' : 'Show video controls'}
      />

      <Animated.View
        style={[styles.overlay, { opacity: chromeOpacity }]}
        pointerEvents={chromeVisible ? 'box-none' : 'none'}
      >
        <View style={styles.controlBar}>
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
      marginTop: CONTROLS_GAP,
    },
    container: {
      backgroundColor: colors.bg.theater,
    },
    // A transparent strip, not a card: the only thing that dims the transport is
    // the viewer's gradient behind it, so the frame is never boxed in
    // (KNOT-020k-fix4). Its width matches the story text's column.
    controlBar: {
      bottom: BAR_BOTTOM,
      left: spacing.lg,
      paddingBottom: CONTROLS_PADDING,
      position: 'absolute',
      right: spacing.lg,
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

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
import { formatDuration } from './feedVideoMeta';
import { scrubSeconds, viewerProgress } from './mediaViewer';

/**
 * Whether a video starts muted.
 *
 * The app's rule for a video that starts itself is that it must never make a
 * sound (FeedVideo). A viewer opened on purpose is a different context — the
 * person chose to watch — but the tap that opened it did not ask for audio, so
 * it starts muted and the unmute control is the first thing in the bottom bar
 * (KNOT-020k). Flip this one constant to start with sound.
 */
const START_MUTED = true;

/** The centre play/pause disc. */
const PLAY_DISC_SIZE = 68;
const PLAY_ICON_SIZE = 30;

/** The mute control's disc and glyph. */
const MUTE_DISC_SIZE = 44;
const MUTE_ICON_SIZE = 20;

/** The scrub track's rendered height, and its touch band. */
const TRACK_HEIGHT = 4;
const TRACK_TOUCH_HEIGHT = 28;

type VideoPlayerProps = {
  /** The resolved, absolute URL of the video. */
  readonly uri: string;
  /**
   * True while this page is the one on screen. It is the only input that starts
   * or stops playback, so a gallery of videos plays exactly one at a time.
   */
  readonly isActive: boolean;
  /** The duration the payload knows, in milliseconds, to seed the total time. */
  readonly durationMs?: number | null;
};

/**
 * The media viewer's video page: a full-bleed player with its own controls.
 *
 * The platform controls are deliberately unused — they are a light, stock sheet
 * that breaks the theatre — so this draws its own overlay: a large play/pause
 * disc in the centre and, along the bottom, the current time, a draggable scrub
 * bar, the total duration and a mute toggle. Tapping the video toggles the
 * overlay; the tap therefore never closes the viewer, which is the gallery's
 * swipe-down gesture.
 *
 * The overlay fades with `Animated` on the native driver. The scrub bar is a
 * `PanResponder`: a touch anywhere on its band seeks there on release, and a drag
 * previews the target time before committing — so the bar follows the finger
 * without flooding the player with seeks it cannot serve.
 */
export default function VideoPlayer({
  uri,
  isActive,
  durationMs,
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

  // The overlay's visibility is state (it decides `pointerEvents`), while its
  // fade is an animated value on the native driver.
  const [controlsVisible, setControlsVisible] = useState(true);
  const controlsOpacity = useRef(new Animated.Value(1)).current;

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

  const fadeControls = useCallback(
    (visible: boolean): void => {
      setControlsVisible(visible);
      Animated.timing(controlsOpacity, {
        toValue: visible ? 1 : 0,
        duration: motion.durations.small,
        easing: Easing.bezier(...motion.easing.standard),
        useNativeDriver: true,
      }).start();
    },
    [controlsOpacity],
  );

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
          previewAt(event.nativeEvent.locationX);
        },
        onPanResponderMove: (event) => {
          previewAt(event.nativeEvent.locationX);
        },
        onPanResponderRelease: () => {
          seeking.current = false;
          const target = previewRef.current;
          previewRef.current = null;
          setPreview(null);
          if (target !== null) {
            seek(target);
          }
        },
        onPanResponderTerminate: () => {
          seeking.current = false;
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
    <View style={styles.container}>
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

      {/* The tap layer: it sits under the overlay, so a tap on the picture toggles
          the controls while a tap on a control still reaches the control. */}
      <Pressable
        style={StyleSheet.absoluteFill}
        onPress={() => fadeControls(!controlsVisible)}
        accessibilityRole="button"
        accessibilityLabel={controlsVisible ? 'Hide video controls' : 'Show video controls'}
      />

      <Animated.View
        style={[styles.overlay, { opacity: controlsOpacity }]}
        pointerEvents={controlsVisible ? 'auto' : 'none'}
      >
        <Pressable
          style={styles.playDisc}
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

        <View style={styles.bottomBar}>
          <Text style={[typeScale.label, tabularNums, styles.time]}>
            {formatDuration(shownTime) || '0:00'}
          </Text>

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
            <View style={styles.track}>
              <View style={[styles.trackFill, { width: `${progress * 100}%` }]} />
              <View style={[styles.thumb, { left: `${progress * 100}%` }]} />
            </View>
          </View>

          <Text style={[typeScale.label, tabularNums, styles.time]}>
            {formatDuration(duration) || '0:00'}
          </Text>

          <Pressable
            style={styles.muteDisc}
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
      </Animated.View>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    bottomBar: {
      alignItems: 'center',
      bottom: spacing.xxxl,
      flexDirection: 'row',
      gap: spacing.md,
      left: spacing.xl,
      position: 'absolute',
      right: spacing.xl,
    },
    container: {
      backgroundColor: colors.bg.theater,
      flex: 1,
    },
    // The controls live on the black page, so a scrim disc behind each glyph is
    // what keeps it legible over a bright frame.
    loader: {
      ...StyleSheet.absoluteFillObject,
      alignItems: 'center',
      justifyContent: 'center',
    },
    muteDisc: {
      alignItems: 'center',
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.pill,
      height: MUTE_DISC_SIZE,
      justifyContent: 'center',
      width: MUTE_DISC_SIZE,
    },
    overlay: {
      ...StyleSheet.absoluteFillObject,
    },
    playDisc: {
      alignItems: 'center',
      alignSelf: 'center',
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.pill,
      height: PLAY_DISC_SIZE,
      justifyContent: 'center',
      position: 'absolute',
      top: '50%',
      marginTop: -PLAY_DISC_SIZE / 2,
      width: PLAY_DISC_SIZE,
    },
    thumb: {
      backgroundColor: colors.text.onMedia,
      borderRadius: radius.pill,
      height: 12,
      marginLeft: -6,
      marginTop: (TRACK_HEIGHT - 12) / 2,
      position: 'absolute',
      top: 0,
      width: 12,
    },
    time: {
      color: colors.text.onMedia,
      minWidth: 40,
      textAlign: 'center',
    },
    track: {
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.pill,
      height: TRACK_HEIGHT,
      overflow: 'visible',
      width: '100%',
    },
    trackFill: {
      backgroundColor: colors.text.onMedia,
      borderRadius: radius.pill,
      height: TRACK_HEIGHT,
    },
    // A thin visual track inside a tall touch band, so the bar is easy to grab
    // without a heavy line across the picture (KNOT-020k).
    trackTouch: {
      flex: 1,
      height: TRACK_TOUCH_HEIGHT,
      justifyContent: 'center',
    },
    video: {
      ...StyleSheet.absoluteFillObject,
    },
  });

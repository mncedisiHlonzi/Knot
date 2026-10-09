import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Animated,
  Dimensions,
  FlatList,
  Modal,
  NativeScrollEvent,
  NativeSyntheticEvent,
  PanResponder,
  Pressable,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import Video from 'react-native-video';

import type { StoryMedia } from '../api/stories';
import { API_BASE_URL } from '../config/api';
import { colors, fontSizes, fontWeights, radius, spacing } from '../theme';
import CameraCaptureBadge from './CameraCaptureBadge';

const { width: SCREEN_WIDTH, height: SCREEN_HEIGHT } = Dimensions.get('window');

/** The largest zoom the pinch gesture allows. */
const MAX_SCALE = 4;
/** How far a downward drag must travel to dismiss the viewer. */
const DISMISS_DISTANCE = 120;
/** How long a second tap must follow the first to count as a double-tap. */
const DOUBLE_TAP_MS = 300;

/** Resolves a relative media path returned by the API against the base URL. */
function resolveMediaUrl(url: string): string {
  return `${API_BASE_URL}${url}`;
}

/** Formats a number of seconds as m:ss. */
function formatTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return '0:00';
  }
  const whole = Math.floor(seconds);
  const minutes = Math.floor(whole / 60);
  const rest = whole % 60;
  return `${minutes}:${rest.toString().padStart(2, '0')}`;
}

type ZoomableImageProps = {
  readonly uri: string;
};

/**
 * A pinch-to-zoom, pan-while-zoomed image.
 *
 * It uses only React Native's built-in `Animated` and `PanResponder` — no gesture
 * library (KNOT-ADR-037). The responder deliberately declines single-finger moves
 * while the image is at rest so the surrounding pager keeps the horizontal swipe;
 * it claims the gesture only for a two-finger pinch or for a drag while already
 * zoomed. A double tap resets the zoom.
 */
function ZoomableImage({ uri }: ZoomableImageProps): React.ReactElement {
  const [loading, setLoading] = useState(true);

  const scale = useRef(new Animated.Value(1)).current;
  const translateX = useRef(new Animated.Value(0)).current;
  const translateY = useRef(new Animated.Value(0)).current;
  const opacity = useRef(new Animated.Value(0)).current;

  const scaleValue = useRef(1);
  const translate = useRef({ x: 0, y: 0 });
  const gestureStart = useRef({ x: 0, y: 0 });
  const pinchStartDistance = useRef<number | null>(null);
  const pinchStartScale = useRef(1);
  const lastTapAt = useRef(0);

  const reset = useCallback((): void => {
    scaleValue.current = 1;
    translate.current = { x: 0, y: 0 };
    pinchStartDistance.current = null;
    Animated.parallel([
      Animated.spring(scale, { toValue: 1, useNativeDriver: true }),
      Animated.spring(translateX, { toValue: 0, useNativeDriver: true }),
      Animated.spring(translateY, { toValue: 0, useNativeDriver: true }),
    ]).start();
  }, [scale, translateX, translateY]);

  const panResponder = useMemo(
    () =>
      PanResponder.create({
        onStartShouldSetPanResponder: () => false,
        onMoveShouldSetPanResponder: (event, gesture) => {
          if (event.nativeEvent.touches.length >= 2) {
            return true;
          }
          return scaleValue.current > 1 && (Math.abs(gesture.dx) > 2 || Math.abs(gesture.dy) > 2);
        },
        onPanResponderGrant: () => {
          gestureStart.current = { ...translate.current };
          pinchStartDistance.current = null;
        },
        onPanResponderMove: (event, gesture) => {
          const touches = event.nativeEvent.touches;
          if (touches.length >= 2) {
            const [first, second] = touches;
            const distance = Math.hypot(first.pageX - second.pageX, first.pageY - second.pageY);
            if (pinchStartDistance.current === null) {
              pinchStartDistance.current = distance;
              pinchStartScale.current = scaleValue.current;
            } else if (pinchStartDistance.current > 0) {
              const next = Math.min(
                Math.max(pinchStartScale.current * (distance / pinchStartDistance.current), 1),
                MAX_SCALE,
              );
              scaleValue.current = next;
              scale.setValue(next);
            }
            return;
          }

          if (scaleValue.current > 1) {
            const nextX = gestureStart.current.x + gesture.dx;
            const nextY = gestureStart.current.y + gesture.dy;
            translate.current = { x: nextX, y: nextY };
            translateX.setValue(nextX);
            translateY.setValue(nextY);
          }
        },
        onPanResponderRelease: () => {
          gestureStart.current = { ...translate.current };
          pinchStartDistance.current = null;
          if (scaleValue.current <= 1.01) {
            reset();
          }
        },
        onPanResponderTerminate: () => {
          pinchStartDistance.current = null;
        },
      }),
    [reset, scale, translateX, translateY],
  );

  const handleTap = useCallback((): void => {
    const now = Date.now();
    if (now - lastTapAt.current < DOUBLE_TAP_MS) {
      lastTapAt.current = 0;
      reset();
    } else {
      lastTapAt.current = now;
    }
  }, [reset]);

  return (
    <View style={styles.page} {...panResponder.panHandlers}>
      <Pressable style={styles.page} onPress={handleTap}>
        {loading ? <ActivityIndicator style={styles.loader} color={colors.text.secondary} /> : null}
        <Animated.Image
          source={{ uri }}
          style={[
            styles.fullMedia,
            { opacity, transform: [{ scale }, { translateX }, { translateY }] },
          ]}
          resizeMode="contain"
          onLoadEnd={() => {
            setLoading(false);
            Animated.timing(opacity, {
              toValue: 1,
              duration: 200,
              useNativeDriver: true,
            }).start();
          }}
        />
      </Pressable>
    </View>
  );
}

type VideoPlayerProps = {
  readonly uri: string;
};

/**
 * A video with a custom control overlay: play/pause, a scrub bar that seeks on
 * tap, and a current/total time readout. The overlay is used instead of the
 * platform's built-in controls so it matches the app's dark theme and sits
 * consistently under the browser-style top bar.
 */
function VideoPlayer({ uri }: VideoPlayerProps): React.ReactElement {
  const videoRef = useRef<React.ComponentRef<typeof Video>>(null);
  const [paused, setPaused] = useState(false);
  const [current, setCurrent] = useState(0);
  const [duration, setDuration] = useState(0);
  const [trackWidth, setTrackWidth] = useState(0);
  const [loading, setLoading] = useState(true);

  const seek = useCallback((seconds: number): void => {
    videoRef.current?.seek(seconds);
    setCurrent(seconds);
  }, []);

  const progress = duration > 0 ? Math.min(Math.max(current / duration, 0), 1) : 0;

  return (
    <View style={styles.page}>
      <Video
        ref={videoRef}
        source={{ uri }}
        style={styles.fullMedia}
        paused={paused}
        resizeMode="contain"
        onLoad={(data) => {
          setDuration(data.duration);
          setLoading(false);
        }}
        onProgress={(data) => setCurrent(data.currentTime)}
        onEnd={() => setPaused(true)}
      />

      {loading ? <ActivityIndicator style={styles.loader} color={colors.text.secondary} /> : null}

      <View style={styles.controls}>
        <Pressable
          style={styles.playButton}
          onPress={() => setPaused((value) => !value)}
          accessibilityRole="button"
          accessibilityLabel={paused ? 'Play video' : 'Pause video'}
        >
          <Text style={styles.playButtonText}>{paused ? '▶' : '❚❚'}</Text>
        </Pressable>

        <Pressable
          style={styles.track}
          onLayout={(event) => setTrackWidth(event.nativeEvent.layout.width)}
          onPress={(event) => {
            if (trackWidth > 0 && duration > 0) {
              const ratio = Math.min(Math.max(event.nativeEvent.locationX / trackWidth, 0), 1);
              seek(ratio * duration);
            }
          }}
          accessibilityRole="adjustable"
          accessibilityLabel="Video position"
        >
          <View style={[styles.trackFill, { width: `${progress * 100}%` }]} />
        </Pressable>

        <Text style={styles.time}>{`${formatTime(current)} / ${formatTime(duration)}`}</Text>
      </View>
    </View>
  );
}

type MediaGalleryModalProps = {
  readonly visible: boolean;
  readonly media: readonly StoryMedia[];
  readonly initialIndex: number;
  readonly onClose: () => void;
};

/**
 * A full-screen, swipeable gallery of a story's media.
 *
 * A horizontal `FlatList` with paging moves between items; a downward drag
 * dismisses the whole viewer. Images support pinch-zoom and double-tap-to-reset,
 * and videos carry a play/scrub overlay. It is a single modal over the story
 * detail, so it needs no navigation route of its own.
 */
export default function MediaGalleryModal({
  visible,
  media,
  initialIndex,
  onClose,
}: MediaGalleryModalProps): React.ReactElement {
  const [index, setIndex] = useState(initialIndex);
  const translateY = useRef(new Animated.Value(0)).current;

  useEffect(() => {
    if (visible) {
      setIndex(initialIndex);
      translateY.setValue(0);
    }
  }, [visible, initialIndex, translateY]);

  const dismiss = useCallback((): void => {
    Animated.timing(translateY, {
      toValue: SCREEN_HEIGHT,
      duration: 180,
      useNativeDriver: true,
    }).start(() => onClose());
  }, [onClose, translateY]);

  const dismissResponder = useMemo(
    () =>
      PanResponder.create({
        onMoveShouldSetPanResponder: (_event, gesture) =>
          Math.abs(gesture.dy) > 12 && Math.abs(gesture.dy) > Math.abs(gesture.dx),
        onPanResponderMove: (_event, gesture) => {
          if (gesture.dy > 0) {
            translateY.setValue(gesture.dy);
          }
        },
        onPanResponderRelease: (_event, gesture) => {
          if (gesture.dy > DISMISS_DISTANCE) {
            dismiss();
          } else {
            Animated.spring(translateY, { toValue: 0, useNativeDriver: true }).start();
          }
        },
      }),
    [dismiss, translateY],
  );

  const onMomentumScrollEnd = (event: NativeSyntheticEvent<NativeScrollEvent>): void => {
    const next = Math.round(event.nativeEvent.contentOffset.x / SCREEN_WIDTH);
    if (next !== index) {
      setIndex(next);
    }
  };

  const currentItem = index >= 0 && index < media.length ? media[index] : undefined;

  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      <Animated.View
        style={[styles.backdrop, { transform: [{ translateY }] }]}
        {...dismissResponder.panHandlers}
      >
        <FlatList
          // Remounting on open places the list at the tapped item without a
          // scroll animation flashing on screen.
          key={visible ? `open-${initialIndex}` : 'closed'}
          data={media}
          keyExtractor={(item) => item.id}
          horizontal
          pagingEnabled
          showsHorizontalScrollIndicator={false}
          initialScrollIndex={initialIndex}
          getItemLayout={(_data, i) => ({
            length: SCREEN_WIDTH,
            offset: SCREEN_WIDTH * i,
            index: i,
          })}
          onMomentumScrollEnd={onMomentumScrollEnd}
          renderItem={({ item }) =>
            item.media_type === 'video' ? (
              <VideoPlayer uri={resolveMediaUrl(item.url)} />
            ) : (
              <ZoomableImage uri={resolveMediaUrl(item.url)} />
            )
          }
        />

        {currentItem?.source === 'camera' ? (
          <CameraCaptureBadge label="Captured" style={styles.badge} />
        ) : null}

        {media.length > 1 ? (
          <Text style={styles.counter}>{`${index + 1} / ${media.length}`}</Text>
        ) : null}

        <Pressable
          style={styles.close}
          onPress={onClose}
          accessibilityRole="button"
          accessibilityLabel="Close media viewer"
        >
          <Text style={styles.closeText}>Close</Text>
        </Pressable>
      </Animated.View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: {
    backgroundColor: colors.bg.primary,
    flex: 1,
  },
  badge: {
    left: spacing.xl,
    position: 'absolute',
    top: spacing['3xl'],
  },
  close: {
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    position: 'absolute',
    right: spacing.xl,
    top: spacing['3xl'],
  },
  closeText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  controls: {
    alignItems: 'center',
    bottom: spacing['3xl'],
    flexDirection: 'row',
    left: spacing.xl,
    position: 'absolute',
    right: spacing.xl,
  },
  counter: {
    alignSelf: 'center',
    bottom: spacing.lg,
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    position: 'absolute',
  },
  fullMedia: {
    height: SCREEN_HEIGHT,
    width: SCREEN_WIDTH,
  },
  loader: {
    position: 'absolute',
  },
  page: {
    alignItems: 'center',
    height: SCREEN_HEIGHT,
    justifyContent: 'center',
    width: SCREEN_WIDTH,
  },
  playButton: {
    alignItems: 'center',
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.pill,
    borderWidth: 1,
    height: 40,
    justifyContent: 'center',
    width: 40,
  },
  playButtonText: {
    color: colors.text.primary,
    fontSize: fontSizes.sm,
  },
  time: {
    color: colors.text.primary,
    fontSize: fontSizes.sm,
    marginLeft: spacing.md,
    minWidth: 84,
    textAlign: 'right',
  },
  track: {
    backgroundColor: colors.bg.surface,
    borderRadius: radius.pill,
    flex: 1,
    height: 6,
    marginLeft: spacing.md,
    overflow: 'hidden',
  },
  trackFill: {
    backgroundColor: colors.brand.purple,
    height: 6,
  },
});

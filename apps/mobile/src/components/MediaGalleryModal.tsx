import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Animated,
  Easing,
  FlatList,
  Modal,
  PanResponder,
  Platform,
  Pressable,
  StatusBar,
  StyleSheet,
  Text,
  View,
  useWindowDimensions,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from 'react-native';
import { X } from 'phosphor-react-native';

import { API_BASE_URL } from '../config/api';
import { setNavigationBarColor } from '../native/systemBars';
import { motion, radius, spacing, typeScale, useTheme, useThemedStyles } from '../theme';
import type { ThemeColors } from '../theme';
import { clampViewerIndex } from './mediaViewer';
import VideoPlayer from './VideoPlayer';

/** One item the viewer can show. Story media satisfies this shape directly. */
export type ViewerMedia = {
  readonly id: string;
  readonly media_type: 'image' | 'video';
  /** The API-relative path (or an absolute URL, which is used verbatim). */
  readonly url: string;
  readonly width?: number | null;
  readonly height?: number | null;
  readonly duration_ms?: number | null;
};

/** The largest zoom a pinch or a double tap can reach. */
const MAX_SCALE = 4;
/** The zoom a double tap applies. */
const DOUBLE_TAP_SCALE = 2.5;
/** How long a second tap must follow the first to count as a double tap. */
const DOUBLE_TAP_MS = 280;
/** How far a downward drag must travel before the viewer closes. */
const DISMISS_DISTANCE = 120;

/** The close control's disc and glyph. */
const CLOSE_DISC_SIZE = 44;
const CLOSE_ICON_SIZE = 20;

/**
 * The top chrome's inset. A modal draws its own window, so the app's
 * `SafeAreaView` does not apply; Android reports its status-bar height, and iOS
 * gets a fixed inset that clears the notch and the Dynamic Island.
 */
const TOP_INSET =
  Platform.OS === 'android'
    ? (StatusBar.currentHeight ?? 0) + spacing.md
    : spacing.xxxl + spacing.md;

/** Resolves an API-relative media path, leaving an absolute URL untouched. */
function resolveUri(url: string): string {
  return /^https?:\/\//.test(url) ? url : `${API_BASE_URL}${url}`;
}

/** Keeps a pan inside the zoomed image's own edges. */
function clampOffset(value: number, pageSize: number, scale: number): number {
  const limit = (pageSize * (scale - 1)) / 2;
  return Math.min(Math.max(value, -limit), limit);
}

type ZoomableImageProps = {
  readonly uri: string;
  readonly width: number;
  readonly height: number;
};

/**
 * One image page: pinch to zoom, drag while zoomed, double tap to zoom in on the
 * touched point or to reset.
 *
 * It uses only React Native's built-in `Animated` and `PanResponder` — the app
 * has no gesture library and this task adds none (KNOT-ADR-037, KNOT-020k). The
 * responder declines a single-finger move while the image is at rest, so the
 * horizontal pager keeps the swipe and the gallery keeps its swipe-down dismiss;
 * it claims the gesture only for a two-finger pinch or a drag while zoomed.
 * Because it claims those first, a zoomed image never moves the pager under the
 * finger.
 */
function ZoomableImage({ uri, width, height }: ZoomableImageProps): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  const [loading, setLoading] = useState(true);

  const scale = useRef(new Animated.Value(1)).current;
  const translateX = useRef(new Animated.Value(0)).current;
  const translateY = useRef(new Animated.Value(0)).current;
  const opacity = useRef(new Animated.Value(0)).current;

  // The responders read refs, never the render's values.
  const scaleValue = useRef(1);
  const offset = useRef({ x: 0, y: 0 });
  const offsetStart = useRef({ x: 0, y: 0 });
  const pinchStartDistance = useRef<number | null>(null);
  const pinchStartScale = useRef(1);
  const lastTapAt = useRef(0);

  const springTo = useCallback(
    (nextScale: number, x: number, y: number): void => {
      scaleValue.current = nextScale;
      offset.current = { x, y };
      Animated.parallel([
        Animated.spring(scale, {
          toValue: nextScale,
          useNativeDriver: true,
          speed: 18,
          bounciness: 4,
        }),
        Animated.spring(translateX, {
          toValue: x,
          useNativeDriver: true,
          speed: 18,
          bounciness: 4,
        }),
        Animated.spring(translateY, {
          toValue: y,
          useNativeDriver: true,
          speed: 18,
          bounciness: 4,
        }),
      ]).start();
    },
    [scale, translateX, translateY],
  );

  const reset = useCallback((): void => {
    pinchStartDistance.current = null;
    springTo(1, 0, 0);
  }, [springTo]);

  /** Zooms in on the touched point, so the detail under the finger stays put. */
  const zoomAt = useCallback(
    (x: number, y: number): void => {
      const nextX = clampOffset((width / 2 - x) * (DOUBLE_TAP_SCALE - 1), width, DOUBLE_TAP_SCALE);
      const nextY = clampOffset(
        (height / 2 - y) * (DOUBLE_TAP_SCALE - 1),
        height,
        DOUBLE_TAP_SCALE,
      );
      springTo(DOUBLE_TAP_SCALE, nextX, nextY);
    },
    [height, springTo, width],
  );

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
          offsetStart.current = { ...offset.current };
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
            const nextX = clampOffset(
              offsetStart.current.x + gesture.dx,
              width,
              scaleValue.current,
            );
            const nextY = clampOffset(
              offsetStart.current.y + gesture.dy,
              height,
              scaleValue.current,
            );
            offset.current = { x: nextX, y: nextY };
            translateX.setValue(nextX);
            translateY.setValue(nextY);
          }
        },
        onPanResponderRelease: () => {
          pinchStartDistance.current = null;
          // A pinch released below the resting scale settles back rather than
          // leaving a shrunken image on screen.
          if (scaleValue.current <= 1.01) {
            reset();
          } else if (scaleValue.current < 1) {
            springTo(1, 0, 0);
          }
        },
        onPanResponderTerminate: () => {
          pinchStartDistance.current = null;
        },
      }),
    [height, reset, scale, springTo, translateX, translateY, width],
  );

  const handleTap = useCallback(
    (event: { nativeEvent: { locationX: number; locationY: number } }): void => {
      const now = Date.now();
      if (now - lastTapAt.current < DOUBLE_TAP_MS) {
        lastTapAt.current = 0;
        if (scaleValue.current > 1.01) {
          reset();
        } else {
          zoomAt(event.nativeEvent.locationX, event.nativeEvent.locationY);
        }
        return;
      }
      lastTapAt.current = now;
    },
    [reset, zoomAt],
  );

  return (
    <View style={[styles.page, { height, width }]} {...panResponder.panHandlers}>
      <Pressable style={styles.pageFill} onPress={handleTap} accessibilityRole="image">
        {loading ? <View style={styles.loader} /> : null}
        <Animated.Image
          source={{ uri }}
          style={[
            styles.pageFill,
            { opacity, transform: [{ scale }, { translateX }, { translateY }] },
          ]}
          resizeMode="contain"
          accessibilityIgnoresInvertColors
          onLoadEnd={() => {
            setLoading(false);
            Animated.timing(opacity, {
              toValue: 1,
              duration: motion.durations.medium,
              easing: Easing.bezier(...motion.easing.standard),
              useNativeDriver: true,
            }).start();
          }}
        />
      </Pressable>
    </View>
  );
}

type MediaGalleryModalProps = {
  /** Whether the viewer is showing. */
  readonly visible: boolean;
  /** The items to page through. */
  readonly media: readonly ViewerMedia[];
  /** The item the viewer opens on. Out-of-range values are clamped. */
  readonly initialIndex: number;
  /** Called when the viewer is dismissed, by the close control or a swipe. */
  readonly onClose: () => void;
};

/**
 * The full-screen media viewer: a theatre, not a page.
 *
 * A black backdrop, a horizontal pager of `contain`-fitted images and custom
 * video pages, and a swipe-down dismiss. It is an RN `Modal`, so it owns its
 * window and renders above the tab bar and every overlay; the app passes it a
 * list and an index, and it owns everything else (KNOT-020k).
 *
 * The dismiss gesture is a `PanResponder` on the pager's container that claims
 * only a downward, mostly vertical drag. Horizontal movement is left to the
 * pager, and a zoomed image claims the gesture before this container sees it, so
 * a swipe means "close" only when nothing else wants the touch.
 */
export default function MediaGalleryModal({
  visible,
  media,
  initialIndex,
  onClose,
}: MediaGalleryModalProps): React.ReactElement {
  const { colors, theme } = useTheme();
  const styles = useThemedStyles(createStyles);
  const { width, height } = useWindowDimensions();
  const [index, setIndex] = useState(() => clampViewerIndex(initialIndex, media.length));
  const translateY = useRef(new Animated.Value(0)).current;

  const safeIndex = clampViewerIndex(index, media.length);

  // Opening at a different item, or reopening the same one, always starts at the
  // top of the screen and on the requested page.
  useEffect(() => {
    if (visible) {
      setIndex(clampViewerIndex(initialIndex, media.length));
      translateY.setValue(0);
    }
  }, [visible, initialIndex, media.length, translateY]);

  // A theatre's system chrome is dark: the navigation bar goes black while the
  // viewer is open and returns to the theme's canvas when it closes (KNOT-020k).
  useEffect(() => {
    if (!visible) {
      return undefined;
    }
    setNavigationBarColor(colors.bg.theater, false);
    return () => setNavigationBarColor(colors.bg.canvas, theme === 'light');
  }, [colors, theme, visible]);

  const dismiss = useCallback((): void => {
    Animated.timing(translateY, {
      toValue: height,
      duration: motion.durations.medium,
      easing: Easing.bezier(...motion.easing.exit),
      useNativeDriver: true,
    }).start(({ finished }) => {
      if (finished) {
        onClose();
      }
    });
  }, [height, onClose, translateY]);

  // The room darkens as the viewer is dragged away, so the drag reads as leaving
  // the theatre rather than sliding a white panel.
  const backdropOpacity = translateY.interpolate({
    inputRange: [0, height * 0.5],
    outputRange: [1, 0],
    extrapolate: 'clamp',
  });

  const dismissResponder = useMemo(
    () =>
      PanResponder.create({
        onMoveShouldSetPanResponder: (_event, gesture) =>
          gesture.dy > 8 && Math.abs(gesture.dy) > Math.abs(gesture.dx) * 1.2,
        onPanResponderMove: (_event, gesture) => {
          if (gesture.dy > 0) {
            translateY.setValue(gesture.dy);
          }
        },
        onPanResponderRelease: (_event, gesture) => {
          if (gesture.dy > DISMISS_DISTANCE || gesture.vy > 0.9) {
            dismiss();
          } else {
            Animated.spring(translateY, {
              toValue: 0,
              speed: 20,
              bounciness: 3,
              useNativeDriver: true,
            }).start();
          }
        },
        onPanResponderTerminate: () => {
          Animated.spring(translateY, {
            toValue: 0,
            speed: 20,
            bounciness: 3,
            useNativeDriver: true,
          }).start();
        },
      }),
    [dismiss, translateY],
  );

  const handleScrollEnd = (event: NativeSyntheticEvent<NativeScrollEvent>): void => {
    if (width <= 0) {
      return;
    }
    const next = clampViewerIndex(event.nativeEvent.contentOffset.x / width, media.length);
    if (next !== index) {
      setIndex(next);
    }
  };

  const counter = media.length > 1 ? `${safeIndex + 1} / ${media.length}` : null;

  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      statusBarTranslucent
      onRequestClose={onClose}
    >
      {/* The viewer is its own window: declare the light status icons for the
          black room. Unmounting restores the app's own declaration. */}
      <StatusBar barStyle="light-content" backgroundColor="transparent" translucent />

      <Animated.View style={[styles.room, { opacity: backdropOpacity }]} />
      <Animated.View
        style={[styles.sheet, { transform: [{ translateY }] }]}
        {...dismissResponder.panHandlers}
      >
        <FlatList
          // Remounting on open lands on the tapped item without a scroll
          // animation flashing across the screen.
          key={visible ? `open-${initialIndex}` : 'closed'}
          data={media}
          keyExtractor={(item) => item.id}
          horizontal
          pagingEnabled
          showsHorizontalScrollIndicator={false}
          initialScrollIndex={safeIndex}
          extraData={safeIndex}
          getItemLayout={(_data, i) => ({
            length: width,
            offset: width * i,
            index: i,
          })}
          onMomentumScrollEnd={handleScrollEnd}
          renderItem={({ item, index: pageIndex }) =>
            item.media_type === 'video' ? (
              <VideoPlayer
                uri={resolveUri(item.url)}
                isActive={pageIndex === safeIndex}
                durationMs={item.duration_ms}
              />
            ) : (
              <ZoomableImage uri={resolveUri(item.url)} width={width} height={height} />
            )
          }
        />

        <View style={[styles.topBar, { top: TOP_INSET }]} pointerEvents="box-none">
          {counter === null ? null : (
            <View style={styles.counterPill} pointerEvents="none">
              <Text style={[typeScale.label, styles.counterText]}>{counter}</Text>
            </View>
          )}

          <Pressable
            style={styles.close}
            onPress={onClose}
            accessibilityRole="button"
            accessibilityLabel="Close media viewer"
            hitSlop={spacing.sm}
          >
            <X color={colors.text.onMedia} size={CLOSE_ICON_SIZE} weight="bold" />
          </Pressable>
        </View>
      </Animated.View>
    </Modal>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    // The black room behind the pager; its opacity follows the dismiss drag.
    room: {
      ...StyleSheet.absoluteFillObject,
      backgroundColor: colors.bg.theater,
    },
    // The pager's surface is transparent, so the room (and its fade) shows
    // through while the sheet slides.
    sheet: {
      ...StyleSheet.absoluteFillObject,
    },
    page: {
      alignItems: 'center',
      justifyContent: 'center',
    },
    pageFill: {
      height: '100%',
      width: '100%',
    },
    // A quiet grey placeholder until the image decodes, so a slow load is a calm
    // frame rather than a flash of black.
    loader: {
      ...StyleSheet.absoluteFillObject,
      backgroundColor: colors.bg.sunken,
    },
    topBar: {
      alignItems: 'center',
      flexDirection: 'row',
      justifyContent: 'center',
      left: spacing.xl,
      position: 'absolute',
      right: spacing.xl,
    },
    counterPill: {
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.pill,
      paddingHorizontal: spacing.md,
      paddingVertical: spacing.xs,
    },
    counterText: {
      color: colors.text.onMedia,
    },
    close: {
      alignItems: 'center',
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.pill,
      height: CLOSE_DISC_SIZE,
      justifyContent: 'center',
      position: 'absolute',
      right: 0,
      width: CLOSE_DISC_SIZE,
    },
  });

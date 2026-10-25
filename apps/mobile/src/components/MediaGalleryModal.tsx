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
  type TextStyle,
} from 'react-native';
import { ArrowLeft } from 'phosphor-react-native';

import { API_BASE_URL } from '../config/api';
import {
  clearStatusBarIcons,
  setNavigationBarColor,
  setStatusBarIcons,
} from '../native/systemBars';
import {
  fontWeights,
  layout,
  motion,
  radius,
  spacing,
  tabularNums,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../theme';
import type { ThemeColors } from '../theme';
import { formatRelativeTimeShort } from '../utils/time';
import {
  activeViewableKey,
  VIEWABILITY_CONFIG,
  type ViewableItemsInfo,
} from '../utils/viewability';
import Avatar from './Avatar';
import { clampViewerIndex, viewerIndexForId, viewerSnippet } from './mediaViewer';
import VideoPlayer, { VIDEO_CONTROLS_HEIGHT } from './VideoPlayer';
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

/**
 * The story context the viewer's overlay presents.
 *
 * It is a projection of the story (or the adaptation) the media belongs to, not
 * the whole payload: only what the overlay draws, so a screen can hand over its
 * own record without the viewer needing to know any API type. `storyId` is the
 * story the "Read more" link opens, which for an adaptation is the story it
 * adapts rather than the version itself (KNOT-020k-fix2).
 */
export type ViewerStory = {
  /** The story the "Read more" link opens. */
  readonly storyId: string;
  /** The title drawn under the author line. */
  readonly title: string;
  /** The body the overlay quotes three lines of. */
  readonly body: string;
  /**
   * Every item the story has, in display order.
   *
   * The pager works from this array alone, so a surface that can only describe the
   * item it was showing would give the reader a one-page gallery. `App` resolves
   * the full array before opening the viewer when a caller holds a preview only
   * (KNOT-020k-fix4).
   */
  readonly media: readonly ViewerMedia[];
  readonly authorDisplayName?: string | null;
  readonly authorAvatarUrl?: string | null;
  readonly createdAt?: string | null;
};

/** The largest zoom a pinch or a double tap can reach. */
const MAX_SCALE = 4;
/** The zoom a double tap applies. */
const DOUBLE_TAP_SCALE = 2.5;
/** How long a second tap must follow the first to count as a double tap. */
const DOUBLE_TAP_MS = 280;
/** How far a downward drag must travel before the viewer closes. */
const DISMISS_DISTANCE = 120;

/**
 * How many lines of the story the overlay quotes. It ends with the
 * "Read more" link, so three lines is what reads as a paragraph rather than a
 * fragment (KNOT-020k-fix3).
 */
const CONTEXT_BODY_LINES = 3;

/**
 * How far the overlay's text shadow spreads, in points.
 *
 * The overlay has no surface of its own, so the shadow is what keeps the words
 * legible over a white shirt or a bright sky — the TikTok/Instagram treatment. A
 * radius of five is the smallest that stays readable on pure white without turning
 * the glyphs into smudges (KNOT-020k-fix5, KNOT-020k-fix10).
 */
const TEXT_SHADOW_RADIUS = 5;

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
  /**
   * The story the media belongs to, which the overlay presents. When it is
   * omitted the viewer is a pure theatre: the media and a close control.
   */
  readonly story?: ViewerStory | null;
  /** Called when the viewer is dismissed, by the close control or a swipe. */
  readonly onClose: () => void;
  /**
   * Called by the overlay's "Read more" link, after the viewer has closed
   * itself, so the caller only has to navigate (KNOT-020k-fix2).
   */
  readonly onOpenStory?: (storyId: string) => void;
};

/**
 * The full-screen media viewer: a theatre, not a page.
 *
 * A black room, the media edge to edge behind the status bar, and two floating
 * pieces of chrome: a top bar (back on the left, page counter on the right) and
 * — when it was given a story — a transparent block at the bottom carrying one
 * bold author line, the title, three lines of the body and a bold "Read more"
 * link (KNOT-020k, KNOT-020k-fix4, KNOT-020k-fix5).
 *
 * The overlay draws **no surface at all** — no band, no scrim, no gradient. Each
 * line is media-white ink with a heavy black shadow, so the picture runs untouched
 * to the bottom edge of the screen and the black a viewer sees under the caption
 * is the system navigation bar and the room, never a panel drawn here (KNOT-020k-fix5,
 * KNOT-020k-fix10). The band an earlier pass added was a misreading of "black at the
 * bottom": it covered the media instead of completing the room around it.
 *
 * The overlay's geometry is nonetheless **the same on every page**: the caption sits
 * at one height whatever the media is, because an image reserves the space a video
 * needs for its transport. Paging between a photo and a clip therefore keeps the
 * text still (KNOT-020k-fix9, KNOT-020k-fix10).
 *
 * The one tappable thing in the overlay is the story link, which closes the
 * viewer and opens the story.
 *
 * ## Gestures
 *
 * - Horizontal: the pager. A zoomed image claims the touch first, so a zoomed
 *   picture pans instead of paging.
 * - Downward on the media: dismiss the viewer, fading the room out as it leaves.
 *
 * ## Chrome
 *
 * For a video the top bar, the player's control bar and the context overlay hide
 * together on a tap of the picture, and return on the next tap (or on paging).
 * An image keeps its chrome, because a tap there is the zoom gesture.
 *
 * ## Mounting
 *
 * The modal itself is mounted for the session, but the interactive stage is
 * mounted **per open** (`MediaGalleryModal` renders `ViewerStage` only while
 * visible). That is what lets the pager seed its active page from `initialIndex`:
 * the first frame of a newly opened viewer already knows which page it is on, so
 * a video there starts playing immediately instead of waiting for a scroll event
 * that a freshly mounted list never emits (KNOT-020k-fix3).
 *
 * It is an RN `Modal`, so it owns its window and renders above the tab bar and
 * every overlay; the app passes it the media, an index and the story context, and
 * it owns everything else.
 */
export default function MediaGalleryModal({
  visible,
  media,
  initialIndex,
  story,
  onClose,
  onOpenStory,
}: MediaGalleryModalProps): React.ReactElement {
  const { colors, theme } = useTheme();

  // A theatre's system chrome is dark: the **system** navigation bar goes black
  // while the viewer is open and returns to the theme's canvas when it closes, and
  // the status bar is painted black with white icons. Those two bars are what
  // complete the room behind the media — the overlay itself draws nothing
  // (KNOT-020k-fix7, KNOT-020k-fix8, KNOT-020k-fix10).
  //
  // The paint has to reach the modal's own `Dialog` window as well as the Activity:
  // React Native's `StatusBar` only ever reaches the Activity, and a dialog does
  // not inherit the Activity's status-bar colour, so forcing the icons alone left
  // the bar white on any screen that had not already declared a transparent bar —
  // which is exactly the Feed, the one viewer entry point with no screen-level
  // `<StatusBar>` of its own (KNOT-020k-fix15). The native module therefore takes
  // the fill colour and paints both windows.
  //
  // `onShow` (below) runs the same paint again once the dialog is actually up:
  // this effect fires in the commit that *builds* the dialog, so on the heaviest
  // mount in the app — the Feed's, which also mounts the whole stage and its media
  // — the one-shot call can arrive before the window is attached, and the walk
  // that finds dialogs would then find none.
  const applyTheaterChrome = useCallback((): void => {
    setNavigationBarColor(colors.bg.theater, false);
    setStatusBarIcons(true, colors.bg.theater);
  }, [colors]);

  useEffect(() => {
    if (!visible) {
      return undefined;
    }
    applyTheaterChrome();
    return () => {
      setNavigationBarColor(colors.bg.canvas, theme === 'light');
      clearStatusBarIcons();
    };
  }, [applyTheaterChrome, colors, theme, visible]);

  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      statusBarTranslucent
      // The dialog's own window is the one that paints the bars while it is up, and
      // it exists only after this callback fires. Re-applying there is what makes
      // the chrome deterministic instead of dependent on commit ordering
      // (KNOT-020k-fix15).
      onShow={applyTheaterChrome}
      onRequestClose={onClose}
    >
      {/* The viewer is its own window: declare the light status icons for the
          black room, transparent so the theatre shows through. This is the iOS
          path and the Activity's own declaration; on Android the native module
          above also paints the modal's dialog window, which a component cannot
          reach. Unmounting restores the app's own declaration. */}
      <StatusBar barStyle="light-content" backgroundColor="transparent" translucent />

      {/* A fresh stage per open, so every page index — including the active one —
          starts from `initialIndex` rather than from a state left over from the
          last time the viewer was shown (KNOT-020k-fix3). */}
      {visible ? (
        <ViewerStage
          key={`${initialIndex}:${media.length}`}
          media={media}
          initialIndex={initialIndex}
          story={story}
          onClose={onClose}
          onOpenStory={onOpenStory}
        />
      ) : null}
    </Modal>
  );
}

type ViewerStageProps = {
  readonly media: readonly ViewerMedia[];
  readonly initialIndex: number;
  readonly story?: ViewerStory | null;
  readonly onClose: () => void;
  readonly onOpenStory?: (storyId: string) => void;
};

/**
 * The theatre itself: the room, the pager, the top bar and the story overlay.
 *
 * Exported so the chrome contract (back control on the left, a transparent story
 * overlay at one fixed height) can be tested by calling it directly, which is the
 * project's renderer-less test pattern (KNOT-020k-fix6, KNOT-020k-fix7).
 */
export function ViewerStage({
  media,
  initialIndex,
  story,
  onClose,
  onOpenStory,
}: ViewerStageProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const { width, height } = useWindowDimensions();
  const translateY = useRef(new Animated.Value(0)).current;
  const listRef = useRef<FlatList<ViewerMedia>>(null);

  // The page on screen. Seeding it from `initialIndex` — rather than from zero —
  // is the fix for a video that would not play when the viewer was opened on it:
  // the first render already marks that page active, so the player mounts
  // playing, and `onViewableItemsChanged` only has to keep it in step afterwards
  // (KNOT-020k-fix3).
  const [activeIndex, setActiveIndex] = useState(() =>
    clampViewerIndex(initialIndex, media.length),
  );

  // The chrome a video's tap hides: the top bar, the control bar and the
  // overlay; its opacity is animated so the change reads as a fade.
  const [chromeVisible, setChromeVisible] = useState(true);
  const chromeOpacity = useRef(new Animated.Value(1)).current;

  const safeIndex = clampViewerIndex(activeIndex, media.length);
  const currentItem = media[safeIndex];
  const videoPage = currentItem !== undefined && currentItem.media_type === 'video';
  const counter = media.length > 1 ? `${safeIndex + 1} / ${media.length}` : null;
  const snippet = viewerSnippet(story?.body ?? '');

  // The overlay's author line is one bold statement, so the fields are resolved
  // once here rather than inside the JSX (KNOT-020k-fix4).
  const authorName =
    typeof story?.authorDisplayName === 'string' ? story.authorDisplayName.trim() : '';
  const avatarPath = typeof story?.authorAvatarUrl === 'string' ? story.authorAvatarUrl.trim() : '';
  const avatarUri = avatarPath === '' ? undefined : `${API_BASE_URL}${avatarPath}`;
  const timestamp =
    typeof story?.createdAt === 'string' && story.createdAt !== ''
      ? formatRelativeTimeShort(story.createdAt)
      : '';
  // The page the viewer was opened on. It is fixed for the life of the stage: the
  // pager starts here and the reader's swipes take over (KNOT-020k-fix4).
  const initialPage = useRef(clampViewerIndex(initialIndex, media.length)).current;

  // A freshly mounted list also reports what it is showing once, and that report
  // can arrive before the initial offset has settled. The seeded page is already
  // correct on the first frame, so a report is only trusted once the pager has
  // actually moved — otherwise a stale "page one" report could pause the video
  // the viewer opened on (KNOT-020k-fix3).
  const pagerMoved = useRef(false);

  // The viewability config and its callback must keep their identity for the
  // list's life, or `FlatList` warns and stops reporting — the same rule the Feed
  // follows (KNOT-020c-fix3). The stage is mounted per open with a fixed media
  // list, so closing over it here is safe.
  const viewabilityConfig = useRef(VIEWABILITY_CONFIG).current;
  const onViewableItemsChanged = useRef((info: ViewableItemsInfo): void => {
    if (!pagerMoved.current) {
      return;
    }
    const key = activeViewableKey(info);
    if (key === undefined) {
      return;
    }
    const next = viewerIndexForId(
      media.map((item) => item.id),
      key,
    );
    // A report naming an item this viewer no longer holds is ignored rather than
    // read as "page one".
    if (next >= 0) {
      setActiveIndex(next);
    }
  }).current;

  // `initialScrollIndex` is read once, when the list mounts, and on a freshly
  // mounted pager it occasionally does not take — leaving the viewer on page one
  // while `activeIndex` claimed the tapped page, so a video there never received
  // `isActive` (KNOT-020k-fix4). Asking for the page once, after mount, is the
  // belt to that brace; `getItemLayout` makes the request exact.
  useEffect(() => {
    listRef.current?.scrollToIndex({ index: initialPage, animated: false });
  }, [initialPage]);

  // An image has nothing to toggle, so its chrome stays up; a video's follows the
  // tap. The fade runs on the native driver (KNOT-020k-fix2).
  useEffect(() => {
    Animated.timing(chromeOpacity, {
      toValue: chromeVisible || !videoPage ? 1 : 0,
      duration: motion.durations.small,
      easing: Easing.bezier(...motion.easing.standard),
      useNativeDriver: true,
    }).start();
  }, [chromeOpacity, chromeVisible, videoPage]);

  // Paging always brings the chrome back: someone who just paged wants to see
  // where they are (KNOT-020k-fix2).
  useEffect(() => {
    setChromeVisible(true);
  }, [safeIndex]);

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

  /**
   * Leaves the theatre and opens the story. The viewer closes first, so the
   * caller's navigation pushes onto a clean stack (KNOT-020k-fix3).
   */
  const handleReadMore = (): void => {
    const id = story?.storyId;
    if (id === undefined || id === '' || onOpenStory === undefined) {
      return;
    }
    onClose();
    onOpenStory(id);
  };

  /**
   * The arithmetic half of the active-page rule.
   *
   * `onViewableItemsChanged` is the primary signal, but it can lag a fast fling;
   * once the pager has stopped its offset is exact, so this settles any page the
   * viewability report missed (KNOT-020k-fix3).
   */
  const handleScrollEnd = (event: NativeSyntheticEvent<NativeScrollEvent>): void => {
    if (width <= 0) {
      return;
    }
    pagerMoved.current = true;
    const next = clampViewerIndex(event.nativeEvent.contentOffset.x / width, media.length);
    if (next !== activeIndex) {
      setActiveIndex(next);
    }
  };

  return (
    <>
      <Animated.View style={[styles.room, { opacity: backdropOpacity }]} />

      <Animated.View style={[styles.stage, { transform: [{ translateY }] }]}>
        {/* Only the media owns the dismiss gesture: a downward drag here leaves
            the theatre. */}
        <View style={styles.media} {...dismissResponder.panHandlers}>
          <FlatList
            ref={listRef}
            data={media}
            keyExtractor={(item) => item.id}
            horizontal
            pagingEnabled
            showsHorizontalScrollIndicator={false}
            // The tapped page, on the first frame and after mount. Enough pages
            // render that the video there exists before it is scrolled to.
            initialScrollIndex={initialPage}
            initialNumToRender={initialPage + 1}
            extraData={safeIndex}
            getItemLayout={(_data, i) => ({
              length: width,
              offset: width * i,
              index: i,
            })}
            viewabilityConfig={viewabilityConfig}
            onViewableItemsChanged={onViewableItemsChanged}
            onMomentumScrollEnd={handleScrollEnd}
            renderItem={({ item, index: pageIndex }) =>
              item.media_type === 'video' ? (
                <VideoPlayer
                  uri={resolveUri(item.url)}
                  width={width}
                  height={height}
                  isActive={pageIndex === safeIndex}
                  durationMs={item.duration_ms}
                  chromeVisible={chromeVisible}
                  onToggleChrome={() => setChromeVisible((shown) => !shown)}
                />
              ) : (
                <ZoomableImage uri={resolveUri(item.url)} width={width} height={height} />
              )
            }
          />

          {/* A floating bar, not a band: the back control sits on the left and
              the counter on the right, each on its own scrim disc so it reads
              over any frame. The left-hand arrow matches the rest of the app's
              navigation (KNOT-020k-fix6). */}
          <Animated.View
            style={[styles.topBar, { top: TOP_INSET, opacity: chromeOpacity }]}
            pointerEvents={chromeVisible || !videoPage ? 'box-none' : 'none'}
          >
            <Pressable
              style={styles.close}
              onPress={onClose}
              accessibilityRole="button"
              accessibilityLabel="Close media viewer"
              hitSlop={spacing.sm}
            >
              <ArrowLeft color={colors.text.onMedia} size={CLOSE_ICON_SIZE} weight="bold" />
            </Pressable>

            {counter === null ? (
              <View style={styles.counterSlot} />
            ) : (
              <View style={styles.counterPill}>
                <Text style={[typeScale.label, tabularNums, styles.counterText]}>{counter}</Text>
              </View>
            )}
          </Animated.View>
        </View>

        {/* The story context: transparent, with nothing drawn behind it. Every
            line is media ink carrying its own shadow, so the picture runs to the
            bottom edge of the screen and the black under the words is the room
            and the system navigation bar (KNOT-020k-fix5, KNOT-020k-fix10).
            An image's caption hugs the bottom edge; a video's is raised by the
            transport it has to clear — and only a video reserves that room
            (KNOT-020k-fix11). */}
        {story != null ? (
          <Animated.View
            style={[
              styles.context,
              videoPage ? styles.contextAboveControls : null,
              { opacity: chromeOpacity },
            ]}
            pointerEvents={chromeVisible || !videoPage ? 'box-none' : 'none'}
          >
            <View style={styles.authorRow}>
              <Avatar
                uri={avatarUri}
                displayName={authorName}
                size="sm"
                accessibilityLabel={
                  authorName === '' ? 'Author profile picture' : `${authorName}'s profile picture`
                }
              />
              {authorName === '' ? null : (
                <Text style={[typeScale.body, styles.authorText]} numberOfLines={1}>
                  <Text style={styles.authorName}>{authorName}</Text>
                  {timestamp === '' ? null : ` · ${timestamp}`}
                </Text>
              )}
            </View>

            <Text style={[typeScale.h3, styles.contextTitle]} numberOfLines={2}>
              {story.title}
            </Text>

            {snippet.text === '' ? null : (
              <Text style={[typeScale.body, styles.contextBody]} numberOfLines={CONTEXT_BODY_LINES}>
                {snippet.text}
              </Text>
            )}

            <Pressable
              style={styles.readMore}
              onPress={handleReadMore}
              hitSlop={spacing.sm}
              accessibilityRole="link"
              accessibilityLabel={`Read more of ${story.title}`}
            >
              <Text style={[typeScale.button, styles.readMoreText]}>Read more</Text>
            </Pressable>
          </Animated.View>
        ) : null}
      </Animated.View>
    </>
  );
}

/**
 * The overlay's ink: media white with a heavy black shadow, drawn straight on the
 * picture.
 *
 * The shadow — not a scrim — is what keeps a line readable over a white shirt or a
 * bright sky, and it is applied to every text node: RN does not pass
 * `textShadow*` down from a parent `Text` to the spans nested inside it, so the
 * author's name needs the same treatment as the line around it (KNOT-020k-fix5).
 */
function overlayText(colors: ThemeColors): TextStyle {
  return {
    color: colors.text.onMedia,
    textShadowColor: colors.bg.theater,
    textShadowOffset: { width: 0, height: 1 },
    textShadowRadius: TEXT_SHADOW_RADIUS,
  };
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    close: {
      alignItems: 'center',
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.pill,
      height: CLOSE_DISC_SIZE,
      justifyContent: 'center',
      width: CLOSE_DISC_SIZE,
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
    // Keeps the counter's slot when a single item hides it, so the close control
    // stays in the same place whether or not the counter is drawn.
    counterSlot: {
      height: CLOSE_DISC_SIZE,
      width: CLOSE_DISC_SIZE,
    },
    // A quiet grey placeholder until the image decodes, so a slow load is a calm
    // frame rather than a flash of black.
    loader: {
      ...StyleSheet.absoluteFillObject,
      backgroundColor: colors.bg.sunken,
    },
    // The pager's own surface: transparent, so the room (and its fade) shows
    // through while the stage slides.
    media: {
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
    // The story context: no surface of its own. The picture stays visible to the
    // bottom edge, and the shadowed ink is what keeps the words readable. The
    // inset is minimal, so an image's caption hugs the bottom of the screen —
    // there is no transport under it to make room for (KNOT-020k-fix11).
    context: {
      bottom: 0,
      gap: spacing.xs,
      left: 0,
      paddingBottom: spacing.lg,
      paddingHorizontal: layout.screenPaddingFeed,
      position: 'absolute',
      right: 0,
    },
    // The only page that reserves room: a video, whose transport sits under the
    // caption. The full transport height plus `lg` of air keeps the text clear of
    // the scrub bar's invisible 32pt touch band — otherwise a tap on "Read more"
    // would seek instead of opening the story. Nothing is filled here; it is an
    // inset, not a panel (KNOT-020k-fix5, KNOT-020k-fix11).
    contextAboveControls: {
      paddingBottom: VIDEO_CONTROLS_HEIGHT + spacing.lg,
    },
    // One bold line: `[avatar] [name] · [time]`, and nothing else — the Rooted
    // badge stays on the surfaces that have room to explain it (KNOT-020k-fix4).
    authorRow: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.sm,
    },
    authorText: overlayText(colors),
    authorName: {
      fontWeight: fontWeights.bold,
    },
    contextBody: overlayText(colors),
    contextTitle: overlayText(colors),
    readMore: {
      alignSelf: 'flex-start',
    },
    // The one control in the overlay: bold brand ink, carrying the same heavy
    // shadow as the rest of the block so it reads over a bright frame without a
    // scrim behind it (KNOT-020k-fix3, KNOT-020k-fix5).
    readMoreText: {
      ...overlayText(colors),
      color: colors.brand.primary,
    },
    // The black room behind the media; its opacity follows the dismiss drag.
    room: {
      ...StyleSheet.absoluteFillObject,
      backgroundColor: colors.bg.theater,
    },
    stage: {
      ...StyleSheet.absoluteFillObject,
    },
    // A floating bar, not a band: two controls at the window's corners with the
    // picture showing between them.
    topBar: {
      alignItems: 'center',
      flexDirection: 'row',
      justifyContent: 'space-between',
      left: spacing.lg,
      position: 'absolute',
      right: spacing.lg,
    },
  });

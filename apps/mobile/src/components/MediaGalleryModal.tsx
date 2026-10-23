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
import {
  layout,
  mediaOverlayText,
  motion,
  radius,
  spacing,
  tabularNums,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../theme';
import type { ThemeColors } from '../theme';
import {
  activeViewableKey,
  VIEWABILITY_CONFIG,
  type ViewableItemsInfo,
} from '../utils/viewability';
import AuthorLine, { type AuthorRootedSummary } from './AuthorLine';
import { clampViewerIndex, viewerIndexForId, viewerSnippet } from './mediaViewer';
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

/**
 * The story context the viewer's overlay presents.
 *
 * It is a projection of the story (or the adaptation) the media belongs to, not
 * the whole payload: only what the overlay draws, so a screen can hand over its
 * own record without the viewer needing to know any API type. `storyId` is the
 * story the "Read more..." link opens, which for an adaptation is the story it
 * adapts rather than the version itself (KNOT-020k-fix2).
 */
export type ViewerStory = {
  /** The story the "Read more..." link opens. */
  readonly storyId: string;
  /** The title drawn under the author line. */
  readonly title: string;
  /** The body the overlay quotes three lines of. */
  readonly body: string;
  readonly authorDisplayName?: string | null;
  readonly authorAvatarUrl?: string | null;
  readonly authorRooted?: AuthorRootedSummary | null;
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
 * "Read more..." link, so three lines is what reads as a paragraph rather than a
 * fragment (KNOT-020k-fix3).
 */
const CONTEXT_BODY_LINES = 3;

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
   * Called by the overlay's "Read more..." link, after the viewer has closed
   * itself, so the caller only has to navigate (KNOT-020k-fix2).
   */
  readonly onOpenStory?: (storyId: string) => void;
};

/**
 * The full-screen media viewer: a theatre, not a page.
 *
 * A black room, the media edge to edge behind the status bar, and two floating
 * pieces of chrome: a top bar (page counter on the left, close on the right) and
 * — when it was given a story — a transparent overlay at the bottom carrying the
 * author, the title, three lines of the body and a bold "Read more..." link
 * (KNOT-020k, KNOT-020k-fix3).
 *
 * The overlay is deliberately **not a surface**: nothing is filled behind it, so
 * the picture runs to the bottom of the screen and the words sit on it. Its ink
 * is the media ink (white plus a shadow) rather than the canvas ink, which is
 * what keeps it legible over a dark photograph. The one tappable thing in it is
 * the link, which closes the viewer and opens the story.
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

  // A theatre's system chrome is dark: the navigation bar goes black while the
  // viewer is open and returns to the theme's canvas when it closes (KNOT-020k).
  useEffect(() => {
    if (!visible) {
      return undefined;
    }
    setNavigationBarColor(colors.bg.theater, false);
    return () => setNavigationBarColor(colors.bg.canvas, theme === 'light');
  }, [colors, theme, visible]);

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

/** The theatre itself: the room, the pager, the top bar and the context overlay. */
function ViewerStage({
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

  // The overlay's own height, measured so a video's control bar floats above it
  // instead of landing behind the text (KNOT-020k-fix3).
  const [contextHeight, setContextHeight] = useState(0);

  const safeIndex = clampViewerIndex(activeIndex, media.length);
  const currentItem = media[safeIndex];
  const videoPage = currentItem !== undefined && currentItem.media_type === 'video';
  const counter = media.length > 1 ? `${safeIndex + 1} / ${media.length}` : null;
  const snippet = viewerSnippet(story?.body ?? '');
  const hasStory = story != null;
  const bottomInset = hasStory ? contextHeight + spacing.md : spacing.xl;

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
                  bottomInset={bottomInset}
                />
              ) : (
                <ZoomableImage uri={resolveUri(item.url)} width={width} height={height} />
              )
            }
          />

          {/* A floating bar, not a band: the counter sits on the left and the
              close control on the right, each on its own scrim disc so it reads
              over any frame (KNOT-020k-fix2). */}
          <Animated.View
            style={[styles.topBar, { top: TOP_INSET, opacity: chromeOpacity }]}
            pointerEvents={chromeVisible || !videoPage ? 'box-none' : 'none'}
          >
            {counter === null ? (
              <View style={styles.counterSlot} />
            ) : (
              <View style={styles.counterPill}>
                <Text style={[typeScale.label, tabularNums, styles.counterText]}>{counter}</Text>
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
          </Animated.View>
        </View>

        {/* The context overlay: no fill, no card, no handle — the story's
            attribution and its opening words sit straight on the picture, with
            one bold link out. `onLayout` publishes its height so a video's
            control bar floats above it rather than behind the text
            (KNOT-020k-fix3). */}
        {story != null ? (
          <Animated.View
            style={[styles.context, { opacity: chromeOpacity }]}
            pointerEvents={chromeVisible || !videoPage ? 'box-none' : 'none'}
            onLayout={(event) => setContextHeight(event.nativeEvent.layout.height)}
          >
            <AuthorLine
              displayName={story.authorDisplayName}
              avatarUrl={story.authorAvatarUrl}
              createdAt={story.createdAt}
              rooted={story.authorRooted ?? null}
              size="medium"
              timeFormat="short"
              tone="media"
            />

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
              <Text style={[typeScale.button, styles.readMoreText]}>Read more...</Text>
            </Pressable>
          </Animated.View>
        ) : null}
      </Animated.View>
    </>
  );
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
    // The context overlay: no surface at all. It sits on the media, so its ink is
    // the media ink and the picture runs behind the words (KNOT-020k-fix3).
    context: {
      bottom: 0,
      gap: spacing.xs,
      left: 0,
      paddingBottom: spacing.xl,
      paddingHorizontal: layout.screenPaddingFeed,
      position: 'absolute',
      right: 0,
    },
    contextBody: mediaOverlayText(colors),
    contextTitle: mediaOverlayText(colors),
    readMore: {
      alignSelf: 'flex-start',
    },
    // The one control in the overlay: bold, brand ink, with the media shadow so it
    // still reads over a bright frame (KNOT-020k-fix3).
    readMoreText: {
      color: colors.brand.primary,
      textShadowColor: colors.scrim.textShadow,
      textShadowOffset: { width: 0, height: 1 },
      textShadowRadius: 2,
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

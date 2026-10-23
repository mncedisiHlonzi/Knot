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

import { EMPTY_REACTION_COUNTS, type ReactionCounts } from '../api/reactions';
import { API_BASE_URL } from '../config/api';
import { setNavigationBarColor } from '../native/systemBars';
import {
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
import AuthorLine, { type AuthorRootedSummary } from './AuthorLine';
import Button from './Button';
import { clampViewerIndex, viewerSnippet } from './mediaViewer';
import ReactionBar, { type ReactionBarEntityType } from './ReactionBar';
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
 * The story context the viewer's bottom sheet presents.
 *
 * It is a projection of the story (or the adaptation) the media belongs to, not
 * the whole payload: only what the sheet draws, so a screen can hand over its
 * own record without the viewer needing to know any API type. `storyId` is the
 * story a reader lands on from "View Story", which for an adaptation is the
 * story it adapts rather than the version itself (KNOT-020k-fix2).
 */
export type ViewerStory = {
  /** The story the "View Story" action opens. */
  readonly storyId: string;
  /** The title drawn under the author line. */
  readonly title: string;
  /** The body the sheet quotes two lines of. */
  readonly body: string;
  readonly authorId?: string;
  readonly authorDisplayName?: string | null;
  readonly authorAvatarUrl?: string | null;
  readonly authorRooted?: AuthorRootedSummary | null;
  readonly createdAt?: string | null;
  /**
   * What the reaction bar reacts to. A story is the default; a version entry
   * passes its own kind and id, because the feed's adaptation card carries the
   * version's signals rather than the story's (KNOT-020k-fix2).
   */
  readonly reactionEntityType?: ReactionBarEntityType;
  readonly reactionEntityId?: string;
  readonly reactions?: ReactionCounts;
  readonly myReactions?: readonly string[];
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
 * The contextual sheet's resting height, as a share of the window, and the strip
 * it leaves showing when it is collapsed to its handle. The media stays visible
 * above (and, collapsed, almost all of it is visible) — the sheet is context, not
 * a wall (KNOT-020k-fix2).
 */
const SHEET_HEIGHT_RATIO = 0.55;
const SHEET_PEEK_HEIGHT = spacing.xxxl;

/** The sheet's grab handle. */
const HANDLE_WIDTH = 36;
const HANDLE_HEIGHT = 4;

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
   * The story the media belongs to, which the contextual sheet presents. When it
   * is omitted the viewer is a pure theatre: the media and a close control.
   */
  readonly story?: ViewerStory | null;
  /** The reader's access token, so the sheet's signals can be saved. */
  readonly token?: string;
  /** Called when the viewer is dismissed, by the close control or a swipe. */
  readonly onClose: () => void;
  /**
   * Called when the sheet's "View Story" action is chosen, after the viewer has
   * closed itself, so the caller only has to navigate (KNOT-020k-fix2).
   */
  readonly onViewStory?: (storyId: string) => void;
};

/**
 * The full-screen media viewer: a theatre, not a page.
 *
 * A black room, the media edge to edge behind the status bar, a floating top bar
 * (page counter on the left, close on the right), and — when it was given a story
 * — a contextual bottom sheet carrying the author, the title, two lines of the
 * body, the story's four perspective signals, and a "View Story" action
 * (KNOT-020k, KNOT-020k-fix2).
 *
 * ## Gestures
 *
 * - Horizontal: the pager. A zoomed image claims the touch first, so a zoomed
 *   picture pans instead of paging.
 * - Downward on the media: dismiss the viewer, fading the room out as it leaves.
 * - Downward on the sheet: collapse it to its handle, so the whole picture is
 *   visible. A tap or an upward drag brings it back. The sheet never dismisses
 *   the viewer — that gesture belongs to the media.
 *
 * ## Chrome
 *
 * For a video the top bar and the player's control bar hide together on a tap of
 * the picture, and return on the next tap (or on paging). An image keeps its
 * chrome, because a tap there is the zoom gesture.
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
  token,
  onClose,
  onViewStory,
}: MediaGalleryModalProps): React.ReactElement {
  const { colors, theme } = useTheme();
  const styles = useThemedStyles(createStyles);
  const { width, height } = useWindowDimensions();
  const [index, setIndex] = useState(() => clampViewerIndex(initialIndex, media.length));
  const translateY = useRef(new Animated.Value(0)).current;

  // The sheet's signal counts live here rather than in the sheet, so an
  // optimistic tap survives the sheet's own re-renders. The `story` prop is kept
  // in a ref because callers build it per render; the seed is read only when the
  // viewer opens (KNOT-020k-fix2).
  const storyRef = useRef<ViewerStory | null | undefined>(story);
  storyRef.current = story;
  const [reactions, setReactions] = useState<ReactionCounts>(
    story?.reactions ?? EMPTY_REACTION_COUNTS,
  );
  const [myReactions, setMyReactions] = useState<readonly string[]>(story?.myReactions ?? []);

  // The chrome a video's tap hides: the top bar and the player's control bar.
  const [chromeVisible, setChromeVisible] = useState(true);
  const chromeOpacity = useRef(new Animated.Value(1)).current;

  // The contextual sheet's detent and drag.
  const [sheetOpen, setSheetOpen] = useState(true);
  const sheetOffset = useRef(new Animated.Value(0)).current;
  const sheetDragStart = useRef(0);

  const sheetHeight = Math.round(height * SHEET_HEIGHT_RATIO);
  const sheetCollapsedOffset = Math.max(sheetHeight - SHEET_PEEK_HEIGHT, 0);
  const hasStory = story != null;
  const sheetInset = !hasStory ? 0 : sheetOpen ? sheetHeight : SHEET_PEEK_HEIGHT;

  const safeIndex = clampViewerIndex(index, media.length);
  const currentItem = media[safeIndex];
  const videoPage = currentItem !== undefined && currentItem.media_type === 'video';
  const counter = media.length > 1 ? `${safeIndex + 1} / ${media.length}` : null;
  const snippet = viewerSnippet(story?.body ?? '');

  // Opening at a different item, or reopening the same one, always starts at the
  // top of the screen, on the requested page, with the chrome and sheet open and
  // the story's own counts.
  useEffect(() => {
    if (!visible) {
      return;
    }
    setIndex(clampViewerIndex(initialIndex, media.length));
    translateY.setValue(0);
    setChromeVisible(true);
    setSheetOpen(true);
    sheetOffset.setValue(0);
    const seed = storyRef.current;
    setReactions(seed?.reactions ?? EMPTY_REACTION_COUNTS);
    setMyReactions(seed?.myReactions ?? []);
  }, [visible, initialIndex, media.length, sheetOffset, translateY]);

  // The sheet arrives with the theatre: it starts below the edge and springs up,
  // so the story context feels attached to the media rather than switched on
  // (KNOT-020k-fix2).
  useEffect(() => {
    if (!visible || !hasStory) {
      return;
    }
    sheetOffset.setValue(sheetHeight);
    Animated.spring(sheetOffset, {
      toValue: 0,
      speed: 16,
      bounciness: 2,
      useNativeDriver: true,
    }).start();
  }, [visible, hasStory, sheetHeight, sheetOffset]);

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

  const settleSheet = useCallback(
    (open: boolean): void => {
      setSheetOpen(open);
      Animated.spring(sheetOffset, {
        toValue: open ? 0 : sheetCollapsedOffset,
        speed: 20,
        bounciness: 2,
        useNativeDriver: true,
      }).start();
    },
    [sheetCollapsedOffset, sheetOffset],
  );

  // The sheet's own gesture: a downward drag collapses it to its handle, an
  // upward drag opens it. It never closes the viewer — that is the media's swipe
  // (KNOT-020k-fix2).
  const sheetResponder = useMemo(
    () =>
      PanResponder.create({
        onMoveShouldSetPanResponder: (_event, gesture) =>
          Math.abs(gesture.dy) > 6 && Math.abs(gesture.dy) > Math.abs(gesture.dx) * 1.2,
        onPanResponderGrant: () => {
          sheetDragStart.current = sheetOpen ? 0 : sheetCollapsedOffset;
        },
        onPanResponderMove: (_event, gesture) => {
          const next = Math.min(
            Math.max(sheetDragStart.current + gesture.dy, 0),
            sheetCollapsedOffset,
          );
          sheetOffset.setValue(next);
        },
        onPanResponderRelease: (_event, gesture) => {
          const dragged = sheetDragStart.current + gesture.dy;
          const open =
            gesture.vy < -0.4
              ? true
              : gesture.vy > 0.4
                ? false
                : dragged < sheetCollapsedOffset / 2;
          settleSheet(open);
        },
        onPanResponderTerminate: () => {
          settleSheet(sheetOpen);
        },
      }),
    [settleSheet, sheetCollapsedOffset, sheetOffset, sheetOpen],
  );

  /**
   * Leaves the theatre and opens the story: the viewer closes first, so the
   * caller's navigation pushes onto a clean stack (KNOT-020k-fix2).
   */
  const handleViewStory = (): void => {
    const id = story?.storyId;
    if (id === undefined || id === '' || onViewStory === undefined) {
      return;
    }
    onClose();
    onViewStory(id);
  };

  const handleScrollEnd = (event: NativeSyntheticEvent<NativeScrollEvent>): void => {
    if (width <= 0) {
      return;
    }
    const next = clampViewerIndex(event.nativeEvent.contentOffset.x / width, media.length);
    if (next !== index) {
      setIndex(next);
    }
  };

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

      <Animated.View style={[styles.stage, { transform: [{ translateY }] }]}>
        {/* Only the media owns the dismiss gesture: a downward drag here leaves
            the theatre, while the same drag on the sheet collapses the sheet. */}
        <View style={styles.media} {...dismissResponder.panHandlers}>
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
                  width={width}
                  height={height}
                  isActive={pageIndex === safeIndex}
                  durationMs={item.duration_ms}
                  chromeVisible={chromeVisible}
                  onToggleChrome={() => setChromeVisible((shown) => !shown)}
                  bottomInset={sheetInset + spacing.md}
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

        {story != null ? (
          <Animated.View
            style={[
              styles.sheet,
              { height: sheetHeight, transform: [{ translateY: sheetOffset }] },
            ]}
            {...sheetResponder.panHandlers}
          >
            <Pressable
              style={styles.sheetHandleHit}
              onPress={() => settleSheet(!sheetOpen)}
              accessibilityRole="button"
              accessibilityLabel={sheetOpen ? 'Collapse story details' : 'Expand story details'}
            >
              <View style={styles.sheetHandle} />
            </Pressable>

            <View style={styles.sheetBody} pointerEvents={sheetOpen ? 'auto' : 'none'}>
              <AuthorLine
                displayName={story.authorDisplayName}
                avatarUrl={story.authorAvatarUrl}
                createdAt={story.createdAt}
                rooted={story.authorRooted ?? null}
                size="medium"
                timeFormat="short"
              />

              <Text style={[typeScale.h3, styles.sheetTitle]} numberOfLines={2}>
                {story.title}
              </Text>

              {snippet.text === '' ? null : (
                <Text style={[typeScale.body, styles.sheetSnippet]} numberOfLines={2}>
                  {snippet.text}
                </Text>
              )}

              {snippet.truncated ? (
                <Pressable
                  style={styles.readMore}
                  onPress={handleViewStory}
                  hitSlop={spacing.sm}
                  accessibilityRole="link"
                  accessibilityLabel={`Read more of ${story.title}`}
                >
                  <Text style={[typeScale.label, styles.readMoreText]}>Read more…</Text>
                </Pressable>
              ) : null}

              <ReactionBar
                entityType={story.reactionEntityType ?? 'story'}
                entityId={story.reactionEntityId ?? story.storyId}
                counts={reactions}
                myReactions={myReactions}
                token={token}
                onChange={(counts, mine) => {
                  setReactions(counts);
                  setMyReactions(mine);
                }}
              />

              {onViewStory === undefined ? null : (
                <View style={styles.viewStory}>
                  <Button
                    variant="primary"
                    fullWidth
                    label="View Story"
                    onPress={handleViewStory}
                  />
                </View>
              )}
            </View>
          </Animated.View>
        ) : null}
      </Animated.View>
    </Modal>
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
    readMore: {
      alignSelf: 'flex-start',
    },
    readMoreText: {
      color: colors.brand.primary,
    },
    // The black room behind the media; its opacity follows the dismiss drag.
    room: {
      ...StyleSheet.absoluteFillObject,
      backgroundColor: colors.bg.theater,
    },
    // The contextual sheet: the app's sheet treatment (canvas/sheet fill, `xxl`
    // top corners) floating over the theatre. It is a surface, so it uses the
    // theme's ink rather than the media ink (KNOT-020k-fix2).
    sheet: {
      backgroundColor: colors.bg.sheet,
      borderTopLeftRadius: radius.xxl,
      borderTopRightRadius: radius.xxl,
      bottom: 0,
      left: 0,
      overflow: 'hidden',
      position: 'absolute',
      right: 0,
    },
    sheetBody: {
      flexShrink: 1,
      gap: spacing.sm,
      paddingBottom: spacing.xl,
      paddingHorizontal: layout.screenPaddingFeed,
    },
    sheetHandle: {
      backgroundColor: colors.border.hairline,
      borderRadius: radius.pill,
      height: HANDLE_HEIGHT,
      width: HANDLE_WIDTH,
    },
    // A 48pt grab band around a 4pt handle, so the sheet is easy to catch.
    sheetHandleHit: {
      alignItems: 'center',
      height: SHEET_PEEK_HEIGHT,
      justifyContent: 'center',
    },
    sheetSnippet: {
      color: colors.text.secondary,
    },
    sheetTitle: {
      color: colors.text.primary,
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
    viewStory: {
      marginTop: spacing.xs,
    },
  });

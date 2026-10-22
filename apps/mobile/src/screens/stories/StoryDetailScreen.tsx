import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Image,
  Platform,
  Pressable,
  ScrollView,
  StatusBar,
  StyleSheet,
  Text,
  View,
  useWindowDimensions,
  type ImageLoadEventData,
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from 'react-native';
import { ArrowLeft, Flag, GitBranch } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { EMPTY_REACTION_COUNTS } from '../../api/reactions';
import { type Story, type StoryMedia, storiesApi } from '../../api/stories';
import { versionsApi } from '../../api/versions';
import AuthorLine from '../../components/AuthorLine';
import Button from '../../components/Button';
import FeedVideo from '../../components/FeedVideo';
import Header from '../../components/Header';
import PillarTag from '../../components/PillarTag';
import ReactionBar from '../../components/ReactionBar';
import ReportModal from '../../components/ReportModal';
import VideoThumbnail from '../../components/VideoThumbnail';
import { API_BASE_URL } from '../../config/api';
import { languageName } from '../../data/languages';
import {
  layout,
  mediaOverlayText,
  radius,
  spacing,
  tabularNums,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../../theme';
import type { ThemeColors, ThemeName } from '../../theme';
import { PLACEHOLDER_MEDIA_ASPECT_RATIO, resolveMediaAspectRatio } from '../../utils/mediaFrame';
import { useMediaAspectRatio } from '../../utils/useMediaAspectRatio';
import { carouselCounter, carouselIndex, carouselPositionLabel } from './detailCarousel';

/** Resolves a relative media path returned by the API against the base URL. */
function resolveMediaUrl(url: string): string {
  return `${API_BASE_URL}${url}`;
}

/** The thumbnail strip's square item (KNOT-025-fix1, enlarged in fix5). */
const THUMBNAIL_SIZE = 64;

/** The flag beside the header's Report label (KNOT-020j-fix2). */
const REPORT_ICON_SIZE = 16;

/** The gap between thumbnail tiles, which also sets the strip's snap stride. */
const THUMBNAIL_GAP = spacing.md;

/**
 * The strip's hard height: the 64pt tile plus the content container's 12pt
 * vertical padding at each end. The strip is clipped to this band, so a native
 * video surface can never grow it or push the text column down (KNOT-025-fix8).
 */
const THUMBNAIL_STRIP_HEIGHT = THUMBNAIL_SIZE + spacing.md * 2;

/** The floating header's own height, matching the `Header` component. */
const HEADER_HEIGHT = 56;

/**
 * The distance from the window's top to the header on this screen. Android lays
 * the app out under the translucent bar, so the header must clear it explicitly;
 * iOS is already inset by the app root's `SafeAreaView`, so it starts at zero.
 */
const HEADER_TOP = Platform.OS === 'android' ? (StatusBar.currentHeight ?? 0) : 0;

/** Status-bar plus header: the band the media may sit behind. */
const HEADER_BAND = HEADER_TOP + HEADER_HEIGHT;

/**
 * The bar is transparent, so Android's icons are white whenever media sits
 * behind it; with no media the page's own canvas is behind the bar, so they stay
 * dark. iOS never puts content behind the bar (the app root's `SafeAreaView`
 * insets the screen), so its icons are always dark over the white strip.
 *
 * Nothing is painted behind the bar: the media runs to the top edge at full
 * brightness, as the review asked (KNOT-025-fix5). React Native cannot give the
 * system's own status icons a shadow, so the white icons take their contrast
 * from the media itself.
 */
function statusBarStyle(hasMedia: boolean, theme: ThemeName): 'light-content' | 'dark-content' {
  // Light ink over media on Android (the band is the photo), and over the dark
  // canvas in dark mode; dark ink on the light canvas (KNOT-020i).
  return theme === 'dark' || (Platform.OS === 'android' && hasMedia)
    ? 'light-content'
    : 'dark-content';
}

/**
 * One page of the Story Detail carousel: a full-width media frame at the shape
 * the media itself reports.
 *
 * The page owns the media's natural-ratio resolution (payload dimensions first,
 * then the media's own load, capped at 4:5 by `resolveMediaAspectRatio`) and
 * reports the result up, because the pager's height follows the ACTIVE page
 * (KNOT-025). A video is handed to `FeedVideo`, so the one player keeps its
 * autoplay and mute behaviour; an image draws directly.
 */
function CarouselPage({
  media,
  width,
  height,
  isActive,
  isMuted,
  onToggleMute,
  onAspectRatio,
}: {
  readonly media: StoryMedia;
  readonly width: number;
  readonly height: number;
  readonly isActive: boolean;
  readonly isMuted: boolean;
  readonly onToggleMute: () => void;
  readonly onAspectRatio: (mediaId: string, ratio: number | null) => void;
}): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  const { aspectRatio, onLoadSize } = useMediaAspectRatio(media.width, media.height);
  const uri = resolveMediaUrl(media.url);

  // The carousel's frame takes the active page's shape, so a page reports the
  // ratio it resolved. `null` means the media has not reported one yet, which the
  // screen ignores rather than overwriting a known ratio.
  useEffect(() => {
    onAspectRatio(media.id, aspectRatio);
  }, [aspectRatio, media.id, onAspectRatio]);

  const handleImageLoad = (event: NativeSyntheticEvent<ImageLoadEventData>): void => {
    const { width: loadedWidth, height: loadedHeight } = event.nativeEvent.source;
    onLoadSize(loadedWidth, loadedHeight);
  };

  return (
    // TODO: Re-enable viewer in a future task. The page is a plain view on
    // purpose: tapping the media must do nothing until the viewer returns
    // (KNOT-025-fix5).
    <View style={[styles.page, { height, width }]}>
      {media.media_type === 'video' ? (
        <FeedVideo
          uri={uri}
          isVisible={isActive}
          isMuted={isMuted}
          onToggleMute={onToggleMute}
          onLoadSize={onLoadSize}
          durationMs={media.duration_ms}
          showTag={false}
          durationPosition="bottom-left"
        />
      ) : (
        <Image
          source={{ uri }}
          style={styles.pageMedia}
          resizeMode="cover"
          onLoad={handleImageLoad}
          accessibilityIgnoresInvertColors
        />
      )}
    </View>
  );
}

type StoryDetailScreenProps = {
  /** The id of the story to read. */
  readonly id: string;
  /**
   * The signed-in user's access token, or undefined when signed out. A reaction
   * needs it; without it the bar invites the reader to sign in.
   */
  readonly token?: string;
  /** Called when the person returns to the feed. */
  readonly onBack: () => void;
  /**
   * Called to adapt this story. The argument is the story's root version id,
   * which is where an adaptation starts.
   */
  readonly onAdapt: (rootVersionId: string) => void;
  /** Called when the person wants to see every version of the story. */
  readonly onViewTree: () => void;
  /**
   * Called to open the conversation on a version. The argument is the version
   * id, which is the story's root version from this screen.
   */
  readonly onConversation: (versionId: string) => void;
  /** Called when the story's author is tapped, to open their profile. */
  readonly onOpenUserProfile: (userId: string) => void;
};

/**
 * One story in full, as an edge-to-edge reading flow.
 *
 * The layout is the KNOT-025 design reference's (Claude mockup v2) with the
 * KNOT-025-fix4 edge-to-edge pass: a full-bleed paging media carousel that runs
 * to the window's top edge, a floating header over it, then a reading column
 * (pillar tag, large title, the Feed's author block, the language-tree button,
 * the body), a hairline-separated action area, and the primary CTA.
 *
 * This screen alone makes the status bar translucent: the media sits behind it
 * at full brightness (no scrim — the review found the band too heavy,
 * KNOT-025-fix5), Android's status icons turn white with it, and the Back/More
 * controls float on their own dark discs so they stay legible over any media. A
 * story with no media keeps dark icons, because the canvas itself is behind the
 * bar. Android is laid out under the bar, so the header clears
 * `StatusBar.currentHeight`; iOS is already inset by the app root's
 * `SafeAreaView`, so its carousel starts at the safe-area top rather than behind
 * the notch (there is no safe-area-inset source in the installed dependencies).
 *
 * The media is not tappable: the full-screen viewer is out of the tap flow
 * until the media pass is signed off, so the carousel pages and the strip tiles
 * are plain views (KNOT-025-fix5).
 *
 * A story that does not exist is reported as a normal error message rather than a
 * crash: the server answers 404 with a readable message, and `describeError`
 * surfaces it verbatim.
 */
export default function StoryDetailScreen({
  id,
  token,
  onBack,
  onAdapt,
  onViewTree,
  onConversation,
  onOpenUserProfile,
}: StoryDetailScreenProps): React.ReactElement {
  const { colors, theme } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [story, setStory] = useState<Story | undefined>(undefined);
  const [versionCount, setVersionCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);
  const [reportVisible, setReportVisible] = useState(false);

  // The carousel's own state: which page is showing, the shape each page has
  // reported, the one mute control, and whether the carousel is still on screen
  // (which pauses its video once the reader scrolls past it).
  const { width: pageWidth } = useWindowDimensions();
  const [activeIndex, setActiveIndex] = useState(0);
  const [ratios, setRatios] = useState<Readonly<Record<string, number>>>({});
  const [isMuted, setIsMuted] = useState(true);
  const [carouselVisible, setCarouselVisible] = useState(true);
  const carouselBottom = useRef(0);
  const stripRef = useRef<ScrollView>(null);

  const media = story?.media ?? [];
  const safeIndex = media.length === 0 ? 0 : Math.min(activeIndex, media.length - 1);
  const activeMedia = media[safeIndex];
  // The frame takes the active media's shape: the shape the page reported, else
  // the payload's own dimensions, else the neutral square placeholder. The 4:5
  // cap is applied inside `resolveMediaAspectRatio` (KNOT-020g-fix7).
  const activeRatio =
    (activeMedia === undefined ? undefined : ratios[activeMedia.id]) ??
    (activeMedia === undefined
      ? undefined
      : resolveMediaAspectRatio(activeMedia.width, activeMedia.height)) ??
    PLACEHOLDER_MEDIA_ASPECT_RATIO;
  const carouselHeight = pageWidth / activeRatio;
  // TODO: Re-enable viewer in a future task. With no viewer, the carousel only
  // pauses when the reader scrolls past it or opens the report sheet
  // (KNOT-025-fix5).
  const videoActive = carouselVisible && !reportVisible;

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      // The story carries its root version's content; the tree carries every
      // version of it, so the count comes from the tree.
      const [storyResult, treeResult] = await Promise.all([
        storiesApi.getStory(id, token),
        versionsApi.getTree(id, token),
      ]);
      setStory(storyResult.story);
      setVersionCount(treeResult.versions.length);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [id, token]);

  useEffect(() => {
    void load();
  }, [load]);

  // A different story starts at its first page: the overlay reuses this component
  // instance when one detail replaces another, so the previous page index and
  // shapes must not leak into the new story.
  useEffect(() => {
    setActiveIndex(0);
    setRatios({});
  }, [id]);

  // Keep the active thumbnail in view: the strip scrolls itself to centre the
  // page the carousel settled on, so a long strip never hides the current item
  // (KNOT-025-fix1).
  useEffect(() => {
    if (media.length <= 1) {
      return;
    }
    const stride = THUMBNAIL_SIZE + THUMBNAIL_GAP;
    const offset =
      layout.screenPaddingFeed + activeIndex * stride + THUMBNAIL_SIZE / 2 - pageWidth / 2;
    stripRef.current?.scrollTo({ x: Math.max(0, offset), animated: true });
  }, [activeIndex, media.length, pageWidth]);

  /** Records a page's resolved shape, ignoring the "not known yet" answer. */
  const handlePageRatio = useCallback((mediaId: string, ratio: number | null): void => {
    if (ratio === null) {
      return;
    }
    setRatios((previous) =>
      previous[mediaId] === ratio ? previous : { ...previous, [mediaId]: ratio },
    );
  }, []);

  /** Remembers where the carousel ends, so vertical scrolling can pause its video. */
  const handleCarouselLayout = (event: LayoutChangeEvent): void => {
    carouselBottom.current = event.nativeEvent.layout.y + event.nativeEvent.layout.height;
  };

  /** Settles the counter on the page a paging swipe landed on. */
  const handleCarouselEnd = (event: NativeSyntheticEvent<NativeScrollEvent>): void => {
    setActiveIndex(carouselIndex(event.nativeEvent.contentOffset.x, pageWidth, media.length));
  };

  /** Pauses the main carousel's video when the reader scrolls past it. */
  const handleScroll = (event: NativeSyntheticEvent<NativeScrollEvent>): void => {
    const visible = event.nativeEvent.contentOffset.y < carouselBottom.current;
    setCarouselVisible((previous) => (previous === visible ? previous : visible));
  };

  const toggleMute = useCallback((): void => {
    setIsMuted((previous) => !previous);
  }, []);

  const language = story === undefined ? '' : languageName(story.language);
  const versionsLabel = `${versionCount} ${versionCount === 1 ? 'version' : 'versions'}`;
  const commentCount = typeof story?.comment_count === 'number' ? story.comment_count : 0;
  const conversationLabel =
    commentCount > 0
      ? `Join the conversation in ${language} · ${commentCount}`
      : `Join the conversation in ${language}`;

  return (
    <View style={styles.container}>
      {/* This screen alone goes edge to edge: the media runs under a transparent
          status bar and the floating header (KNOT-025-fix4). */}
      <StatusBar
        translucent
        backgroundColor="transparent"
        barStyle={statusBarStyle(media.length > 0, theme)}
      />

      <ScrollView
        style={styles.scroll}
        contentContainerStyle={styles.content}
        showsVerticalScrollIndicator={false}
        onScroll={handleScroll}
        scrollEventThrottle={16}
      >
        {loading || error !== undefined ? (
          <View style={[styles.status, { paddingTop: HEADER_BAND + spacing.xl }]}>
            {loading ? <ActivityIndicator /> : null}
            {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}
            {!loading && error !== undefined ? (
              <View style={styles.retry}>
                <Button variant="secondary" label="Try again" onPress={() => void load()} />
              </View>
            ) : null}
          </View>
        ) : null}

        {story !== undefined ? (
          <>
            {media.length > 0 ? (
              <View style={styles.carousel} onLayout={handleCarouselLayout}>
                <ScrollView
                  horizontal
                  pagingEnabled
                  showsHorizontalScrollIndicator={false}
                  onMomentumScrollEnd={handleCarouselEnd}
                  style={{ height: carouselHeight }}
                >
                  {media.map((item, index) => (
                    <CarouselPage
                      key={item.id}
                      media={item}
                      width={pageWidth}
                      height={carouselHeight}
                      isActive={index === safeIndex && videoActive}
                      isMuted={isMuted}
                      onToggleMute={toggleMute}
                      onAspectRatio={handlePageRatio}
                    />
                  ))}
                </ScrollView>

                {media.length > 1 ? (
                  <View
                    style={[styles.counterPill, { top: HEADER_BAND + spacing.sm }]}
                    pointerEvents="none"
                  >
                    <Text style={[typeScale.label, tabularNums, mediaOverlayText(colors)]}>
                      {carouselCounter(safeIndex, media.length)}
                    </Text>
                  </View>
                ) : null}
              </View>
            ) : null}

            {/* The thumbnail strip follows the pager: it scrolls itself to keep
                the active page's tile in view. The tiles are static images or
                placeholders, and tapping one does nothing while the viewer is
                out of the tap flow (KNOT-025-fix1, KNOT-025-fix4, KNOT-025-fix9). */}
            {media.length > 1 ? (
              <ScrollView
                ref={stripRef}
                horizontal
                showsHorizontalScrollIndicator={false}
                style={styles.thumbnailStrip}
                contentContainerStyle={styles.thumbnailContent}
                snapToInterval={THUMBNAIL_SIZE + THUMBNAIL_GAP}
                decelerationRate="fast"
              >
                {media.map((item, index) => {
                  const active = index === safeIndex;

                  return (
                    <View
                      key={item.id}
                      style={[styles.thumbnail, active ? null : styles.thumbnailInactive]}
                      accessibilityRole="image"
                      accessibilityState={{ selected: active }}
                      accessibilityLabel={carouselPositionLabel(
                        item.media_type,
                        index,
                        media.length,
                      )}
                    >
                      {item.media_type === 'video' ? (
                        // `StoryMedia` has no poster field yet, so the optional
                        // prop is omitted and the tile is the sunken placeholder.
                        // A still cannot escape its box (KNOT-025-fix9).
                        <VideoThumbnail size={THUMBNAIL_SIZE} />
                      ) : (
                        <Image
                          source={{ uri: resolveMediaUrl(item.url) }}
                          style={styles.thumbnailMedia}
                          resizeMode="cover"
                          accessibilityIgnoresInvertColors
                        />
                      )}

                      {/* The ring is drawn over the media rather than as the
                          tile's border, so the media keeps its full 64pt
                          square (KNOT-025-fix5). */}
                      <View
                        pointerEvents="none"
                        style={[styles.thumbnailRing, active ? styles.thumbnailRingActive : null]}
                      />
                    </View>
                  );
                })}
              </ScrollView>
            ) : null}

            {media.length === 0 ? <View style={{ height: HEADER_BAND }} /> : null}

            <View style={styles.body}>
              <PillarTag pillar={story.pillar} place={story.approximate_location} variant="plain" />

              <Text style={[typeScale.h1, styles.title]}>{story.title}</Text>

              <AuthorLine
                displayName={story.author_display_name}
                avatarUrl={story.author_avatar_url}
                createdAt={story.created_at}
                rooted={story.author_rooted}
                size="large"
                // The same stacked author block the Feed draws: name, quiet Rooted
                // line, then the compact time beside a 48pt avatar (KNOT-025-fix2).
                layout="stacked"
                timeFormat="short"
                onPress={() => onOpenUserProfile(story.author_id)}
              />

              <Pressable
                style={styles.languageRow}
                onPress={onViewTree}
                accessibilityRole="button"
                accessibilityLabel={`View the language tree, ${versionsLabel}`}
              >
                <View style={styles.languageLeft}>
                  <GitBranch color={colors.brand.primary} size={20} />
                  <Text style={[typeScale.button, styles.languageName]} numberOfLines={1}>
                    {language}
                  </Text>
                </View>
                <Text style={[typeScale.bodyS, tabularNums, styles.versionCount]}>
                  {versionsLabel}
                </Text>
              </Pressable>

              <Text style={[typeScale.reading, styles.storyBody]}>{story.body}</Text>

              {/* The action area: one hairline separates it from the story, the
                  four signals sit in a two-by-two grid so their labels never
                  wrap, then the secondary conversation action and the primary
                  adaptation, with generous air between them (KNOT-025-fix2). */}
              <View style={styles.actionsSection}>
                <ReactionBar
                  layout="grid"
                  entityType="story"
                  entityId={story.id}
                  counts={story.reactions ?? EMPTY_REACTION_COUNTS}
                  myReactions={story.my_reactions ?? []}
                  token={token}
                  onChange={(counts, mine) =>
                    setStory((previous) =>
                      previous === undefined
                        ? previous
                        : { ...previous, my_reactions: mine, reactions: counts },
                    )
                  }
                />

                <View style={styles.actionButtons}>
                  <Button
                    variant="secondary"
                    fullWidth
                    label={conversationLabel}
                    onPress={() => onConversation(story.root_version_id)}
                  />
                  <Button
                    variant="primary"
                    fullWidth
                    label="Tell this to your people"
                    onPress={() => onAdapt(story.root_version_id)}
                  />
                </View>
              </View>
            </View>
          </>
        ) : null}
      </ScrollView>

      <View pointerEvents="box-none" style={[styles.headerOverlay, { top: HEADER_TOP }]}>
        <Header
          left={
            <Pressable
              style={styles.iconButton}
              onPress={onBack}
              accessibilityRole="button"
              accessibilityLabel="Back"
            >
              <ArrowLeft color={colors.text.onMedia} size={24} />
            </Pressable>
          }
          right={
            token !== undefined ? (
              <Pressable
                style={styles.reportButton}
                onPress={() => setReportVisible(true)}
                accessibilityRole="button"
                accessibilityLabel="Report this story"
              >
                <Flag color={colors.text.onMedia} size={REPORT_ICON_SIZE} weight="bold" />
                <Text style={[typeScale.label, styles.reportLabel]}>Report</Text>
              </Pressable>
            ) : null
          }
        />
      </View>

      {story !== undefined && token !== undefined ? (
        <ReportModal
          visible={reportVisible}
          token={token}
          entityType="story"
          entityId={story.id}
          onClose={() => setReportVisible(false)}
          onSubmitted={() => setReportVisible(false)}
        />
      ) : null}

      {/* TODO: Re-enable viewer in a future task. `MediaGalleryModal` is kept for
          that: the component and its wiring are intact, only this screen's
          trigger is gone (KNOT-025-fix5). */}
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    actionButtons: {
      gap: spacing.xl,
      marginTop: spacing.xl,
    },
    // The whole bottom action area shares one hairline above it; the reaction grid
    // and the two buttons then flow with generous vertical air.
    actionsSection: {
      borderTopColor: colors.border.hairline,
      borderTopWidth: 1,
      marginTop: spacing.xl,
      paddingTop: spacing.lg,
    },
    body: {
      paddingHorizontal: layout.screenPaddingReading,
      paddingTop: spacing.lg,
    },
    // The carousel runs edge to edge under the header; its height is set per active
    // page. The sunken fill is what shows while a page's media is loading.
    carousel: {
      backgroundColor: colors.bg.sunken,
    },
    container: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    content: {
      paddingBottom: spacing.xxxl,
    },
    counterPill: {
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.md,
      paddingHorizontal: spacing.sm,
      paddingVertical: spacing.xs,
      position: 'absolute',
      right: spacing.md,
    },
    error: {
      color: colors.state.danger,
      marginTop: spacing.sm,
    },
    // The band behind the status bar and header was removed: the media runs to the
    // top edge at full brightness (KNOT-025-fix5).
    headerOverlay: {
      left: 0,
      position: 'absolute',
      right: 0,
      zIndex: 2,
    },
    // A dark disc keeps the back control's white glyph legible on any media.
    iconButton: {
      alignItems: 'center',
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.pill,
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    // The header's report action: a scrim pill wide enough for the word, because
    // an unlabelled glyph left the reader guessing what the tap would do
    // (KNOT-020j-fix2). White ink stays legible on any photo.
    reportButton: {
      alignItems: 'center',
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.pill,
      flexDirection: 'row',
      gap: spacing.xs,
      height: 36,
      paddingHorizontal: spacing.md,
    },
    reportLabel: {
      color: colors.text.onMedia,
    },
    languageLeft: {
      alignItems: 'center',
      flexDirection: 'row',
      flexShrink: 1,
      gap: spacing.sm,
    },
    languageName: {
      color: colors.text.primary,
      flexShrink: 1,
    },
    // The language-tree row: a bordered, intrinsic-width button whose left side
    // names the language and whose right side counts the versions (KNOT-025).
    languageRow: {
      alignItems: 'center',
      backgroundColor: colors.bg.paper,
      borderColor: colors.border.hairline,
      borderRadius: radius.lg,
      borderWidth: 1,
      flexDirection: 'row',
      height: 48,
      justifyContent: 'space-between',
      marginBottom: spacing.lg,
      marginTop: spacing.lg,
      paddingHorizontal: spacing.md,
    },
    page: {
      backgroundColor: colors.bg.sunken,
      overflow: 'hidden',
    },
    pageMedia: {
      height: '100%',
      width: '100%',
    },
    retry: {
      marginTop: spacing.lg,
    },
    scroll: {
      flex: 1,
    },
    status: {
      paddingHorizontal: layout.screenPaddingReading,
    },
    storyBody: {
      color: colors.text.secondary,
    },
    // The thumbnail strip lives outside the reading column, so it keeps the feed's
    // 16-point gutter while the text keeps the 24-point reading inset.
    // The tile is the media's own 64pt square: the state ring is drawn as an
    // overlay, so a border never eats into the frame (KNOT-025-fix5).
    thumbnail: {
      borderRadius: radius.md,
      height: THUMBNAIL_SIZE,
      overflow: 'hidden',
      width: THUMBNAIL_SIZE,
    },
    thumbnailContent: {
      gap: THUMBNAIL_GAP,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.md,
    },
    thumbnailInactive: {
      opacity: 0.8,
    },
    thumbnailMedia: {
      height: '100%',
      width: '100%',
    },
    // The 2pt ring sits on the tile's edge in every state; only its ink changes.
    thumbnailRing: {
      borderColor: colors.border.hairline,
      borderRadius: radius.md,
      borderWidth: 2,
      bottom: 0,
      left: 0,
      position: 'absolute',
      right: 0,
      top: 0,
    },
    thumbnailRingActive: {
      borderColor: colors.brand.primary,
    },
    // A fixed, clipped band: a horizontal ScrollView is content-sized on its
    // cross axis, so without an explicit height a native video surface that
    // measured itself would grow the strip and push the body down (KNOT-025-fix8).
    // `scrollEnabled` stays true because the strip must still scroll sideways.
    thumbnailStrip: {
      flexGrow: 0,
      height: THUMBNAIL_STRIP_HEIGHT,
      overflow: 'hidden',
    },
    // The editorial rhythm: 8pt above the title, 12pt below it into the author
    // block, then 16pt from the author block to the language-tree row (KNOT-025-fix1).
    title: {
      color: colors.text.primary,
      marginBottom: spacing.md,
      marginTop: spacing.sm,
    },
    versionCount: {
      color: colors.text.tertiary,
      flexShrink: 0,
      marginLeft: spacing.sm,
    },
  });

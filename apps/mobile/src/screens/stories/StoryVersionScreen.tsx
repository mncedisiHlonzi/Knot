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
import { StoryVersion, versionsApi } from '../../api/versions';
import AuthorLine from '../../components/AuthorLine';
import Button from '../../components/Button';
import FeedVideo from '../../components/FeedVideo';
import type { ViewerMedia, ViewerStory } from '../../components/MediaGalleryModal';
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

/** The thumbnail strip's square item, matching the Story Detail (KNOT-025-fix5). */
const THUMBNAIL_SIZE = 64;

/** The flag beside the header's Report label (KNOT-020j-fix2). */
const REPORT_ICON_SIZE = 16;

/** The gap between thumbnail tiles, which also sets the strip's snap stride. */
const THUMBNAIL_GAP = spacing.md;

/** The strip's hard height: the 64pt tile plus 12pt vertical padding at each end. */
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
 * brightness, exactly as on the Story Detail (KNOT-025-fix5, KNOT-026).
 */
function statusBarStyle(hasMedia: boolean, theme: ThemeName): 'light-content' | 'dark-content' {
  // Light ink over media on Android (the band is the photo), and over the dark
  // canvas in dark mode; dark ink on the light canvas (KNOT-020i).
  return theme === 'dark' || (Platform.OS === 'android' && hasMedia)
    ? 'light-content'
    : 'dark-content';
}

/**
 * One page of the Version Detail carousel: a full-width media frame at the shape
 * the media itself reports.
 *
 * A version has no media of its own, so these are the parent story's pages. The
 * page owns the media's natural-ratio resolution (payload dimensions first, then
 * the media's own load, capped at 4:5 by `resolveMediaAspectRatio`) and reports
 * the result up, because the pager's height follows the ACTIVE page. A video is
 * handed to `FeedVideo`, so the one player keeps its autoplay and mute
 * behaviour; an image draws directly (KNOT-025, ported by KNOT-026).
 */
function CarouselPage({
  media,
  width,
  height,
  isActive,
  isMuted,
  onToggleMute,
  onAspectRatio,
  onPress,
}: {
  readonly media: StoryMedia;
  readonly width: number;
  readonly height: number;
  readonly isActive: boolean;
  readonly isMuted: boolean;
  readonly onToggleMute: () => void;
  readonly onAspectRatio: (mediaId: string, ratio: number | null) => void;
  /** Called when the media is tapped, to open the full-screen viewer. */
  readonly onPress: () => void;
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
    // The page opens the full-screen viewer; the pager keeps the horizontal
    // swipe (KNOT-020k).
    <Pressable
      style={[styles.page, { height, width }]}
      onPress={onPress}
      accessibilityRole="imagebutton"
      accessibilityLabel="Open media in the full-screen viewer"
    >
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
    </Pressable>
  );
}

type StoryVersionScreenProps = {
  /** The id of the version to read. */
  readonly versionId: string;
  /**
   * The signed-in user's access token, or undefined when signed out. A reaction
   * needs it; without it the bar invites the reader to sign in.
   */
  readonly token?: string;
  /** Called when the person returns to the tree (or wherever they came from). */
  readonly onBack: () => void;
  /**
   * Called to adapt this version. It is given the version's story and its own id:
   * a version may be adapted from at any point in the tree, so the version itself
   * is the parent of the new telling.
   */
  readonly onAdapt: (storyId: string, parentVersionId: string) => void;
  /** Called when the person wants to read the story the version belongs to. */
  readonly onOpenStory: (storyId: string) => void;
  /**
   * Called to open the conversation on this version. The comment thread needs
   * both ids: the version whose comments are shown and the story it belongs to.
   */
  readonly onConversation: (storyId: string, versionId: string) => void;
  /**
   * Called when the person wants to see every version of the story. The argument
   * is the story id, because the tree is the story's.
   */
  readonly onViewTree: (storyId: string) => void;
  /** Called when the version's author is tapped, to open their profile. */
  readonly onOpenUserProfile: (userId: string) => void;
  /** Called when the media is tapped, to open the full-screen viewer. */
  readonly onOpenMedia: (
    items: readonly ViewerMedia[],
    index: number,
    story?: ViewerStory | null,
  ) => void;
};

/**
 * One version of a story, read on its own.
 *
 * The Language Tree lists versions compactly; this screen is the detail view a row
 * opens onto, and it uses the Story Detail's editorial layout (KNOT-025) so an
 * original and an adaptation read with the same premium rhythm (KNOT-026): a
 * full-bleed paging media carousel that runs to the window's top edge behind a
 * floating header, a static thumbnail strip, then a reading column of plain pillar
 * tag, large title, the Feed's stacked author block (the ADAPTER here), the quiet
 * lineage sentence, a bordered language-tree button, and the body.
 *
 * A version owns no media, pillar, place, or version count of its own: those are
 * inherited from the parent story. The current server does not repeat them on
 * `GET /versions/{id}`, so the screen reads the parent story once as enrichment
 * (see `api/versions.ts`); if that read fails the version itself is still
 * readable, just without its inherited presentation.
 *
 * The actions are a version's own: a two-by-two reaction grid in a
 * hairline-separated action area, a ghost "View original story" that opens the
 * story's root detail, and the primary "Adapt this version" CTA that starts a new
 * telling from this exact version.
 *
 * A missing or blocked version is reported as a normal error message rather than a
 * crash: the server answers 404 with a readable message, and `describeError`
 * surfaces it verbatim.
 */
export default function StoryVersionScreen({
  versionId,
  token,
  onBack,
  onAdapt,
  onOpenStory,
  onConversation,
  onViewTree,
  onOpenUserProfile,
  onOpenMedia,
}: StoryVersionScreenProps): React.ReactElement {
  const { colors, theme } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [version, setVersion] = useState<StoryVersion | undefined>(undefined);
  // The parent story, read for the fields a version inherits. It is enrichment:
  // the version's own text is readable without it.
  const [story, setStory] = useState<Story | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);
  const [reportVisible, setReportVisible] = useState(false);

  // The carousel's own state, mirroring the Story Detail: which page is showing,
  // the shape each page has reported, the one mute control, and whether the
  // carousel is still on screen (which pauses its video).
  const { width: pageWidth } = useWindowDimensions();
  const [activeIndex, setActiveIndex] = useState(0);
  const [ratios, setRatios] = useState<Readonly<Record<string, number>>>({});
  const [isMuted, setIsMuted] = useState(true);
  const [carouselVisible, setCarouselVisible] = useState(true);
  const carouselBottom = useRef(0);
  const stripRef = useRef<ScrollView>(null);

  // The inherited presentation: a future server may repeat these on the version,
  // and the current one does not, so the parent story is the fallback (KNOT-026).
  const media = version?.media ?? story?.media ?? [];
  // The viewer's overlay: the adaptation's own content and author, with the story's
  // gallery it is showing. "Read more..." opens the story the version adapts
  // (KNOT-020k-fix4).
  const viewerStory: ViewerStory | undefined =
    version === undefined
      ? undefined
      : {
          storyId: version.story_id,
          title: version.title,
          body: version.body,
          media,
          authorDisplayName: version.author_display_name,
          authorAvatarUrl: version.author_avatar_url,
          createdAt: version.created_at,
        };
  const pillar = version?.pillar ?? story?.pillar;
  const place = version?.approximate_location ?? story?.approximate_location;
  const versionCount = version?.version_count ?? story?.version_count ?? 0;

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
      const result = await versionsApi.getVersion(versionId, token);
      setVersion(result.version);

      // The inherited media, pillar, place, and version count live on the parent
      // story. Read it once, as enrichment: a failure leaves the version readable
      // without them rather than replacing it with an error (KNOT-026).
      try {
        const parent = await storiesApi.getStory(result.version.story_id, token);
        setStory(parent.story);
      } catch {
        // The version's own content is the page; inherited presentation is not.
      }
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [token, versionId]);

  useEffect(() => {
    void load();
  }, [load]);

  // A different version starts at its first page: the overlay reuses this component
  // instance when one version replaces another, so the previous page index, shapes,
  // and parent story must not leak into the new version.
  useEffect(() => {
    setActiveIndex(0);
    setRatios({});
    setStory(undefined);
  }, [versionId]);

  // Keep the active thumbnail in view, exactly as the Story Detail does: the strip
  // scrolls itself to centre the page the carousel settled on (KNOT-025-fix1).
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

  const adaptationNote = (version?.adaptation_note ?? '').trim();
  const attributionLine = (version?.attribution_line ?? '').trim();
  const language = version === undefined ? '' : languageName(version.language);
  const versionsLabel = `${versionCount} ${versionCount === 1 ? 'version' : 'versions'}`;

  return (
    <View style={styles.container}>
      {/* The Version Detail goes edge to edge exactly like the Story Detail: the
          media runs under a transparent status bar and the floating header. */}
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

        {version !== undefined ? (
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
                      onPress={() => onOpenMedia(media, index, viewerStory)}
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

            {/* The thumbnail strip follows the pager. The tiles are static images
                or placeholders; tapping one does nothing while the viewer is out
                of the tap flow (KNOT-025-fix1, KNOT-025-fix5, KNOT-025-fix9). */}
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
                    <Pressable
                      key={item.id}
                      style={[styles.thumbnail, active ? null : styles.thumbnailInactive]}
                      onPress={() => onOpenMedia(media, index, viewerStory)}
                      accessibilityRole="button"
                      accessibilityState={{ selected: active }}
                      accessibilityLabel={`Open ${carouselPositionLabel(
                        item.media_type,
                        index,
                        media.length,
                      )}`}
                    >
                      {' '}
                      {item.media_type === 'video' ? (
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
                    </Pressable>
                  );
                })}
              </ScrollView>
            ) : null}

            {media.length === 0 ? <View style={{ height: HEADER_BAND }} /> : null}

            <View style={styles.body}>
              {pillar === undefined ? null : (
                <PillarTag pillar={pillar} place={place} variant="plain" />
              )}

              <Text style={[typeScale.h1, styles.title]}>{version.title}</Text>

              {/* The ADAPTER, drawn in the same block the Story Detail gives the
                  original author (KNOT-025-fix2, KNOT-026). */}
              <AuthorLine
                displayName={version.author_display_name}
                avatarUrl={version.author_avatar_url}
                createdAt={version.created_at}
                rooted={version.author_rooted}
                size="large"
                layout="stacked"
                timeFormat="short"
                onPress={() => onOpenUserProfile(version.author_id)}
              />

              {/* Who this version was adapted from, in whose language, and from
                  whom in turn. The server renders the sentence; this screen shows
                  it verbatim and renders nothing when it is absent, so an older
                  server does not leave a blank line (KNOT-021, KNOT-026).

                  It sits in its own block below the WHOLE author line — avatar,
                  name, Rooted line, and time — so it can never crowd or overlap
                  the badge above it (KNOT-027). */}
              {attributionLine === '' ? null : (
                <View style={styles.attributionBlock}>
                  <Text style={[typeScale.caption, styles.attribution]} numberOfLines={2}>
                    {attributionLine}
                  </Text>
                </View>
              )}

              <Pressable
                style={styles.languageRow}
                onPress={() => onViewTree(version.story_id)}
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

              <Text style={[typeScale.reading, styles.storyBody]}>{version.body}</Text>

              {/* A version-only artifact, kept as a quiet note so it does not
                  disturb the Story Detail's rhythm (KNOT-026). */}
              {adaptationNote === '' ? null : (
                <View style={styles.noteBlock}>
                  <Text style={[typeScale.overline, styles.noteLabel]}>Adaptation note</Text>
                  <Text style={[typeScale.bodyS, styles.noteBody]}>{adaptationNote}</Text>
                </View>
              )}

              {/* The action area mirrors the Story Detail: one hairline above it,
                  the four signals in a two-by-two grid, then the version's three
                  actions — conversation, original story, adaptation
                  (KNOT-025-fix2, KNOT-026, KNOT-027). */}
              <View style={styles.actionsSection}>
                <ReactionBar
                  layout="grid"
                  entityType="version"
                  entityId={version.id}
                  counts={version.reactions ?? EMPTY_REACTION_COUNTS}
                  myReactions={version.my_reactions ?? []}
                  token={token}
                  onChange={(counts, mine) =>
                    setVersion((previous) =>
                      previous === undefined
                        ? previous
                        : { ...previous, reactions: counts, my_reactions: mine },
                    )
                  }
                />

                <View style={styles.actionButtons}>
                  <Button
                    variant="secondary"
                    fullWidth
                    label="Join the conversation"
                    onPress={() => onConversation(version.story_id, version.id)}
                  />
                  <Button
                    variant="ghost"
                    fullWidth
                    label="View original story"
                    onPress={() => onOpenStory(version.story_id)}
                  />
                  <Button
                    variant="primary"
                    fullWidth
                    label="Adapt this version"
                    onPress={() => onAdapt(version.story_id, version.id)}
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
                accessibilityLabel="Report this version"
              >
                <Flag color={colors.text.onMedia} size={REPORT_ICON_SIZE} weight="bold" />
                <Text style={[typeScale.label, styles.reportLabel]}>Report</Text>
              </Pressable>
            ) : null
          }
        />
      </View>

      {version !== undefined && token !== undefined ? (
        <ReportModal
          visible={reportVisible}
          token={token}
          entityType="version"
          entityId={version.id}
          onClose={() => setReportVisible(false)}
          onSubmitted={() => setReportVisible(false)}
        />
      ) : null}
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    actionButtons: {
      gap: spacing.xl,
      marginTop: spacing.xl,
    },
    // The whole bottom action area shares one hairline above it, exactly as on the
    // Story Detail (KNOT-025-fix2).
    actionsSection: {
      borderTopColor: colors.border.hairline,
      borderTopWidth: 1,
      marginTop: spacing.xl,
      paddingTop: spacing.lg,
    },
    // The lineage sentence sits in its own block below the whole author line —
    // avatar, name, Rooted line, and time — so it can never crowd or overlap the
    // badge above it. `spacing.md` matches the air the title leaves before the
    // author block (KNOT-021, KNOT-027).
    attributionBlock: {
      marginTop: spacing.md,
    },
    attribution: {
      color: colors.text.tertiary,
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
    headerOverlay: {
      left: 0,
      position: 'absolute',
      right: 0,
      zIndex: 2,
    },
    // A dark disc keeps the white control glyph legible on any media.
    iconButton: {
      alignItems: 'center',
      backgroundColor: colors.scrim.overlay,
      borderRadius: radius.pill,
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    // The header's report action: a scrim pill with the word on it, so the tap's
    // consequence is visible before it happens (KNOT-020j-fix2).
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
    // The adapter's note is the one version-only artifact; a quiet block keeps the
    // Story Detail's rhythm rather than adding a card (KNOT-026).
    noteBlock: {
      marginTop: spacing.lg,
    },
    noteBody: {
      color: colors.text.secondary,
      marginTop: spacing.xs,
    },
    noteLabel: {
      color: colors.text.tertiary,
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
    // The story's own voice: primary ink (pure black in light, pure white in dark)
    // so the body reads at full contrast instead of in the supporting grey
    // (KNOT-020k-fix19).
    storyBody: {
      color: colors.text.primary,
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
    // A fixed, clipped band: a horizontal ScrollView is content-sized on its cross
    // axis, so without an explicit height a native video surface that measured
    // itself would grow the strip and push the body down (KNOT-025-fix8).
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

import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Modal,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  View,
  useWindowDimensions,
} from 'react-native';
import { EyeSlash, Flag, MagnifyingGlass, type Icon } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { FEED_PAGE_SIZE, type FeedItem, storiesApi } from '../../api/stories';
import Avatar from '../../components/Avatar';
import ConfirmSheet from '../../components/ConfirmSheet';
import EmptyState from '../../components/EmptyState';
import FeedEntry from '../../components/FeedEntry';
import type { ViewerMedia, ViewerStory } from '../../components/MediaGalleryModal';
import Header from '../../components/Header';
import Logomark from '../../components/Logomark';
import ReportModal from '../../components/ReportModal';
import TheThread from '../../components/TheThread';
import { API_BASE_URL } from '../../config/api';
import {
  elevation,
  fontWeights,
  layout,
  radius,
  spacing,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../../theme';
import type { ThemeColors } from '../../theme';
import {
  VIEWABILITY_CONFIG,
  type ViewableItemsInfo,
  activeViewableKey,
} from '../../utils/viewability';

/** The logomark's size in the feed header. */
const LOGOMARK_SIZE = 32;

/** The header icon size. */
const ICON_SIZE = 24;

/**
 * The ordering the server is asked for. `for_you` runs the relevance engine for
 * the signed-in reader; `recent` is the chronological merged feed (KNOT-027).
 */
type FeedType = 'recent' | 'for_you';

/**
 * The text tabs above the feed.
 *
 * `For you` and `Latest` choose the ordering the server applies. The pillar and
 * place tabs are still a local selection: the feed endpoint has no pillar or
 * place filter yet, so tapping them changes the highlight but not the request.
 *
 * TODO: Connect pillar and place filters.
 */
const FEED_TABS = ['For you', 'Latest', 'Wonder', 'Heritage', 'Nearby'] as const;

/** The tabs that change the request. A pillar tab leaves the ordering alone. */
const FEED_TYPE_BY_TAB: Readonly<Record<string, FeedType>> = {
  'For you': 'for_you',
  Latest: 'recent',
};

type FeedScreenProps = {
  /** The signed-in user's access token, needed to read the feed. */
  readonly token: string;
  /**
   * The signed-in person, drawn as the header's avatar. Only the two fields the
   * avatar needs are required, so the screen does not depend on the whole `User`
   * record (KNOT-020j).
   */
  readonly viewer: {
    readonly display_name: string;
    /** The API-relative avatar path, or an empty string when there is none. */
    readonly avatar_url: string;
  };
  /** Called when a story is tapped. */
  readonly onOpenStory: (id: string) => void;
  /**
   * Called when an ADAPTATION entry is tapped, with the version's id, so the feed
   * can open the version rather than the story it adapts (KNOT-022).
   */
  readonly onOpenVersion: (versionId: string) => void;
  /** Called when a story's author is tapped, to open their profile. */
  readonly onOpenUserProfile: (userId: string) => void;
  /** Called when the header avatar is tapped, to open the signed-in person's profile. */
  readonly onOpenProfile: () => void;
  /** Called when an entry's media is tapped, to open the full-screen viewer. */
  readonly onOpenMedia: (
    items: readonly ViewerMedia[],
    index: number,
    story?: ViewerStory | null,
  ) => void;
  /** Called when the person opens search. */
  readonly onOpenSearch: () => void;
};

/**
 * The public story feed, newest first, as full-width white cards on a grey canvas.
 *
 * Each entry is a white card spanning the whole feed width, held apart from the
 * next by a compact band of the grey separator surface. The band, not a rule drawn
 * on white and not an inset card, is what separates two stories — the pattern a
 * mature social feed uses (KNOT-023b, KNOT-032, KNOT-032-fix1).
 *
 * The layout pins everything but the list: the header and the five text tabs are
 * fixed siblings ABOVE the FlatList, so only the feed entries scroll beneath them
 * (the header is opaque and static — nothing about it is scroll-linked). The tab
 * row is a plain flex row of five equal-width tabs rather than a horizontal
 * scroller, so the labels are centred, never clipped, and never slide sideways.
 * `For you` and `Latest` change the request's `feed_type`; the pillar and place
 * tabs stay a local selection until the API grows those filters (KNOT-027).
 *
 * Paging is keyset-based: each page returns an opaque `next_cursor` that is sent
 * back verbatim to fetch the following page; an empty cursor ends the list.
 *
 * The page is a MERGED stream: a story and somebody's adaptation of it are the same
 * kind of entry, and each carries the `kind` that says which it is. The feed renders
 * both through `FeedEntry` -- the card takes a `variant` -- and a tap opens what the
 * entry actually is: the story, or the version (KNOT-022).
 */
export default function FeedScreen({
  token,
  viewer,
  onOpenStory,
  onOpenVersion,
  onOpenUserProfile,
  onOpenProfile,
  onOpenMedia,
  onOpenSearch,
}: FeedScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [items, setItems] = useState<readonly FeedItem[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [activeTab, setActiveTab] = useState<string>(FEED_TABS[0]);
  // The ordering the list is showing. A pillar tab is visual-only, so it keeps
  // whatever ordering was last chosen rather than forcing a re-fetch.
  const feedType: FeedType = FEED_TYPE_BY_TAB[activeTab] ?? 'for_you';
  // The one entry the feed judges to be on screen. Its video plays; every other
  // entry's pauses (KNOT-020c-fix3).
  const [activeItemId, setActiveItemId] = useState<string | undefined>(undefined);
  // One mute setting for the whole feed, muted to begin with: a video that starts
  // itself must never make a sound.
  const [isMuted, setIsMuted] = useState(true);
  // The entry whose options sheet is open, if any (KNOT-031).
  const [menuItem, setMenuItem] = useState<FeedItem | undefined>(undefined);
  // The entry being reported. The report modal owns its own form state, so this
  // only says which entry the form is about.
  const [reportItem, setReportItem] = useState<FeedItem | undefined>(undefined);
  // The entry awaiting a hide confirmation, if any (KNOT-032-fix1). The card's `✕`
  // and the sheet's "Hide this post" row both arm this; nothing leaves the feed
  // until the person answers.
  const [pendingHideId, setPendingHideId] = useState<string | undefined>(undefined);
  // The Thread spans the list while a pull-to-refresh is in flight (KNOT-020h).
  const { width: pageWidth } = useWindowDimensions();

  // The viewability config and the callback must keep their identity for the life
  // of the list — FlatList misbehaves if either is re-created — so they are held in
  // refs rather than rebuilt on every render.
  const viewabilityConfig = useRef(VIEWABILITY_CONFIG).current;
  const onViewableItemsChanged = useRef((info: ViewableItemsInfo): void => {
    setActiveItemId(activeViewableKey(info));
  }).current;

  /** Flips the feed-wide mute setting; every video follows it. */
  const toggleMute = useCallback((): void => {
    setIsMuted((current) => !current);
  }, []);

  // The header avatar. The API sends an avatar as a relative path, so it is
  // joined to the API base here; a blank path lets `Avatar` draw the initials
  // rather than an empty frame (KNOT-020j).
  const avatarPath = typeof viewer.avatar_url === 'string' ? viewer.avatar_url.trim() : '';
  const avatarUri = avatarPath === '' ? undefined : `${API_BASE_URL}${avatarPath}`;

  const loadFirstPage = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const page = await storiesApi.listStories({ feedType, limit: FEED_PAGE_SIZE }, token);
      setItems(page.items);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [feedType, token]);

  useEffect(() => {
    void loadFirstPage();
  }, [loadFirstPage]);

  /** Pull-to-refresh: reload the first page from the top. */
  const refresh = useCallback(async (): Promise<void> => {
    setRefreshing(true);
    setError(undefined);

    try {
      const page = await storiesApi.listStories({ feedType, limit: FEED_PAGE_SIZE }, token);
      setItems(page.items);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setRefreshing(false);
    }
  }, [feedType, token]);

  const loadMore = useCallback(async (): Promise<void> => {
    if (nextCursor === '' || loadingMore) {
      return;
    }

    setLoadingMore(true);
    setError(undefined);

    try {
      const page = await storiesApi.listStories(
        { cursor: nextCursor, feedType, limit: FEED_PAGE_SIZE },
        token,
      );
      // Append rather than replace: the cursor is exclusive, so a page never
      // repeats an entry already on screen.
      setItems((existing) => [...existing, ...page.items]);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoadingMore(false);
    }
  }, [feedType, loadingMore, nextCursor, token]);

  /** Closes the options sheet without acting on the entry. */
  const closePostOptions = useCallback((): void => {
    setMenuItem(undefined);
  }, []);

  /**
   * Removes one entry from the list, by id.
   *
   * Both hide paths land here: the options sheet's row, and the card's own
   * one-tap `✕`. Holding the rule in one place keeps them the same rule
   * (KNOT-032).
   */
  const hideEntry = useCallback((id: string): void => {
    setItems((existing) => existing.filter((entry) => entry.id !== id));
  }, []);

  /**
   * Asks before hiding.
   *
   * Both entry points — the card's `✕` and the options sheet's "Hide this post" —
   * only arm the confirmation here. Nothing is removed until the person chooses
   * "Hide story", because a one-tap removal from a scrolling feed is too easy to
   * hit by accident (KNOT-032-fix1).
   */
  const requestHide = useCallback((id: string | undefined): void => {
    // The options sheet, when that is where the request came from, closes as the
    // confirmation opens: two stacked sheets would hide the question behind the
    // choices (KNOT-031).
    setMenuItem(undefined);

    if (id === undefined) {
      return;
    }

    setPendingHideId(id);
  }, []);

  /** Dismisses the confirmation: the story stays exactly where it was. */
  const keepStory = useCallback((): void => {
    setPendingHideId(undefined);
  }, []);

  /**
   * Applies the confirmation.
   *
   * Hiding is local to the session. There is no hide endpoint, so the entry leaves
   * the list but returns on the next refresh or page load — it is a "not now",
   * not a mute, and the app does not pretend otherwise (KNOT-031). This is the
   * only path that removes an entry; `hideEntry` does the removing.
   */
  const confirmHide = useCallback((): void => {
    const target = pendingHideId;
    // Clearing first closes the sheet, and makes a second confirm a no-op even if
    // one somehow arrives (KNOT-032-fix1).
    setPendingHideId(undefined);

    if (target === undefined) {
      return;
    }

    hideEntry(target);
  }, [hideEntry, pendingHideId]);

  /** Hands the entry from the options sheet to the report modal. */
  const reportPost = useCallback((): void => {
    setReportItem(menuItem);
    setMenuItem(undefined);
  }, [menuItem]);

  /**
   * Closes the report modal.
   *
   * The entry stays in place after a report is sent: moderation is asynchronous,
   * so hiding it locally would claim an outcome the app cannot promise.
   */
  const closeReport = useCallback((): void => {
    setReportItem(undefined);
  }, []);

  return (
    <View style={styles.container}>
      {/* The fixed top region: the header and the tab row never scroll and never
          fade. Only the FlatList below them scrolls. */}
      <View style={styles.topBar}>
        <Header
          pinned
          left={
            // The bar's own gutter is `layout.screenPaddingFeed`, the same column
            // the stories sit on, so the mark needs no nudge of its own
            // (KNOT-020g-fix13, KNOT-020j-fix2).
            <Logomark size={LOGOMARK_SIZE} />
          }
          right={
            <View style={styles.headerActions}>
              {/* Search stays a glyph control beside the avatar, the pair the
                  header carries now that the bell has moved to the tab bar
                  (KNOT-020j). */}
              <Pressable
                style={styles.iconButton}
                onPress={onOpenSearch}
                accessibilityRole="button"
                accessibilityLabel="Search stories, versions, and questions"
              >
                <MagnifyingGlass color={colors.text.tertiary} size={ICON_SIZE} />
              </Pressable>
              <View style={styles.avatar}>
                <Avatar
                  uri={avatarUri}
                  displayName={viewer.display_name}
                  size="md"
                  onPress={onOpenProfile}
                  accessibilityLabel="Open your profile"
                  testID="feed-profile-avatar"
                />
              </View>
            </View>
          }
        />

        {/* Five equal-width tabs. Every label takes `typeScale.button` (15 pt
            semibold); the active tab is distinguished by ink and the indigo
            underline rather than by weight, so the row stays confident and even
            (KNOT-020c-fix2). `For you` and `Latest` change the request; the
            pillar and place tabs are visual only (KNOT-027). The row itself draws
            no rule: the separation below it is the feed's own separator band,
            identical to the one between two stories (KNOT-020j-fix2). */}
        <View style={styles.tabs}>
          {FEED_TABS.map((label) => {
            const active = label === activeTab;
            return (
              <Pressable
                key={label}
                style={styles.tab}
                onPress={() => setActiveTab(label)}
                accessibilityRole="button"
                accessibilityState={{ selected: active }}
                accessibilityLabel={label}
              >
                <Text
                  style={[typeScale.button, styles.tabLabel, active ? styles.tabLabelActive : null]}
                  numberOfLines={1}
                >
                  {label}
                </Text>
                {/* The selected tab's underline is drawn inside its own tab, at the
                    row's bottom edge, so only the active tab carries a line. */}
                {active ? <View style={styles.tabUnderline} /> : null}
              </Pressable>
            );
          })}
        </View>
      </View>

      {/* The first separator: the same full-bleed band that holds two stories
          apart (`bg.separator`, 8pt), so tabs, the gap and the first story read as
          one rhythm rather than an inset hairline over a grey band
          (KNOT-020j-fix2). */}
      <View style={styles.feedDivider} testID="feed-tabs-divider" />

      <FlatList
        data={items}
        keyExtractor={(item) => item.id}
        contentContainerStyle={styles.listContent}
        viewabilityConfig={viewabilityConfig}
        onViewableItemsChanged={onViewableItemsChanged}
        // The Thread is the feed's refresh indicator. It is drawn at the top of
        // the list content rather than inside the RefreshControl, because the
        // platform control owns the pull gesture and cannot host a custom
        // indicator (KNOT-020h).
        ListHeaderComponent={
          refreshing ? (
            <View style={styles.refreshingThread} testID="feed-thread">
              <TheThread length={pageWidth} />
            </View>
          ) : null
        }
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={refresh}
            // The Thread is the feed's refresh indicator, so the platform
            // spinner is drawn transparent rather than on top of it
            // (KNOT-020h).
            tintColor="transparent"
            colors={['transparent']}
            progressBackgroundColor={colors.bg.canvas}
          />
        }
        onEndReached={() => {
          void loadMore();
        }}
        onEndReachedThreshold={0.5}
        renderItem={({ item }) => (
          // The card: full width, white, set off by the grey band below it rather
          // than by a rule or a horizontal inset (KNOT-023b, KNOT-032-fix1).
          <View style={styles.card}>
            <FeedEntry
              story={{
                author_avatar_url: item.author_avatar_url,
                author_display_name: item.author_display_name,
                author_rooted: item.author_rooted ?? null,
                comment_count: item.comment_count,
                created_at: item.created_at,
                first_media_type: item.first_media_type ?? null,
                first_media_url: item.first_media_url ?? null,
                first_media_width: item.first_media_width ?? null,
                first_media_height: item.first_media_height ?? null,
                id: item.id,
                // The story a version entry adapts, so the viewer's "View Story"
                // has a story to open; a story entry's own id is the story
                // (KNOT-020k-fix2).
                storyId: item.kind === 'version' ? item.story_id : item.id,
                language: item.language,
                language_count: item.language_count,
                media_count: item.media_count,
                pillar: item.pillar,
                place: item.approximate_location,
                preview: item.body,
                preview_truncated: item.preview_truncated,
                reactions: item.reactions,
                recent_interactions: item.recent_interactions,
                // The original author's identity, only on an adaptation; the card
                // heads a version with it. A story entry has no such fields, so
                // the conditional narrows the union (KNOT-023a).
                root_author_avatar_url:
                  item.kind === 'version' ? item.root_author_avatar_url : undefined,
                root_author_display_name:
                  item.kind === 'version' ? item.root_author_display_name : undefined,
                root_author_id: item.kind === 'version' ? item.root_author_id : undefined,
                // The direct parent's author: the lineage actor the attribution
                // stack credits before the original (KNOT-022-fix2).
                parent_author_avatar_url:
                  item.kind === 'version' ? item.parent_author_avatar_url : undefined,
                parent_author_display_name:
                  item.kind === 'version' ? item.parent_author_display_name : undefined,
                parent_author_id: item.kind === 'version' ? item.parent_author_id : undefined,
                title: item.title,
                version_count: item.version_count,
              }}
              variant={item.kind === 'version' ? 'version' : 'story'}
              versionNumber={item.kind === 'version' ? item.version_number : undefined}
              attributionLine={item.kind === 'version' ? item.attribution_line : undefined}
              // The feed separates cards with its own grey band, so the entry draws
              // no rule of its own (KNOT-023b, KNOT-032).
              showBottomHairline={false}
              isMuted={isMuted}
              isVisible={item.id === activeItemId}
              onToggleMute={toggleMute}
              onPress={() =>
                item.kind === 'version' ? onOpenVersion(item.id) : onOpenStory(item.id)
              }
              onOpenAuthor={() => onOpenUserProfile(item.author_id)}
              onOpenMedia={onOpenMedia}
              onOpenOptions={() => setMenuItem(item)}
              // The one-tap hide asks first: it arms the confirmation rather than
              // removing the entry (KNOT-032-fix1).
              onHide={() => requestHide(item.id)}
            />
          </View>
        )}
        ListEmptyComponent={
          loading ? (
            <ActivityIndicator style={styles.spinner} />
          ) : error !== undefined ? (
            <Text style={styles.error}>{error}</Text>
          ) : (
            <EmptyState
              illustrationName="no-stories"
              headline="Stories will gather here."
              body="Choose the languages you read and we'll start there."
            />
          )
        }
        ListFooterComponent={
          loadingMore ? (
            <ActivityIndicator style={styles.footerSpinner} />
          ) : error !== undefined && items.length > 0 ? (
            <Text style={styles.error}>{error}</Text>
          ) : null
        }
      />

      <PostOptionsSheet
        item={menuItem}
        // The sheet's hide row asks exactly like the card's `✕` does: one shared
        // confirmation, one shared removal (KNOT-032-fix1).
        onHide={() => requestHide(menuItem?.id)}
        onReport={reportPost}
        onClose={closePostOptions}
      />

      <ReportModal
        visible={reportItem !== undefined}
        token={token}
        // A feed entry is either a story or somebody's version of one, so the
        // report names the thing that was actually on screen (KNOT-031).
        entityType={reportItem?.kind === 'version' ? 'version' : 'story'}
        entityId={reportItem?.id ?? ''}
        onClose={closeReport}
        onSubmitted={closeReport}
      />

      {/* The hide confirmation. The copy says "for now" because hiding is
          session-local: the entry returns on the next refresh, and the app must
          not imply a mute it cannot deliver (KNOT-032-fix1). */}
      <ConfirmSheet
        visible={pendingHideId !== undefined}
        title="Hide this story?"
        message="This story will be removed from your feed for now. It will be back the next time the feed refreshes."
        confirmLabel="Hide story"
        cancelLabel="Keep story"
        destructive
        onConfirm={confirmHide}
        onCancel={keepStory}
        testID="hide-confirm"
      />
    </View>
  );
}

/**
 * The post options sheet: the actions a reader can take on one feed entry — hide
 * it from this feed, or report it to the moderators.
 *
 * Its hide row does not hide anything itself: it arms the feed's confirmation and
 * closes, so both entry points ask the same question (KNOT-032-fix1).
 *
 * It is local to this file rather than a shared component, because the feed is the
 * only place it is drawn (the same choice the profile's options sheet makes,
 * KNOT-020g-fix). A sheet rather than an inline popover keeps the rows at a
 * comfortable touch size on any card height and puts Cancel where the thumb
 * expects it (KNOT-031).
 */
function PostOptionsSheet({
  item,
  onHide,
  onReport,
  onClose,
}: {
  readonly item: FeedItem | undefined;
  readonly onHide: () => void;
  readonly onReport: () => void;
  readonly onClose: () => void;
}): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  return (
    <Modal visible={item !== undefined} transparent animationType="slide" onRequestClose={onClose}>
      <Pressable
        style={styles.sheetBackdrop}
        onPress={onClose}
        accessibilityRole="button"
        accessibilityLabel="Close the post options"
      >
        {/* The sheet swallows its own taps, so pressing a row never falls through
            to the backdrop and closes the menu before the action runs. */}
        <Pressable style={styles.sheet} onPress={() => undefined}>
          <View style={styles.sheetHandleArea}>
            <View style={styles.sheetHandle} />
          </View>

          <View style={styles.sheetBody}>
            <PostOptionRow icon={EyeSlash} label="Hide this post" onPress={onHide} />
            <PostOptionRow icon={Flag} label="Report post" danger onPress={onReport} />
          </View>

          <Pressable
            style={styles.sheetCancel}
            onPress={onClose}
            accessibilityRole="button"
            accessibilityLabel="Cancel"
          >
            <Text style={[typeScale.button, styles.sheetCancelText]}>Cancel</Text>
          </Pressable>
        </Pressable>
      </Pressable>
    </Modal>
  );
}

/**
 * One row of the post options sheet: a leading icon and a label, 44pt tall, with
 * `danger` reserved for the action that reports somebody's work.
 */
function PostOptionRow({
  icon,
  label,
  danger = false,
  onPress,
}: {
  readonly icon: Icon;
  readonly label: string;
  readonly danger?: boolean;
  readonly onPress: () => void;
}): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const IconComponent = icon;
  return (
    <Pressable
      style={styles.option}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={label}
    >
      <IconComponent
        color={danger ? colors.state.danger : colors.text.tertiary}
        size={20}
        weight="bold"
      />
      <Text style={[typeScale.body, styles.optionLabel, danger ? styles.optionLabelDanger : null]}>
        {label}
      </Text>
    </Pressable>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    // The avatar sits in the header's own gutter, one step in from the search
    // glyph's 44pt box so the pair reads as an icon and a face, not as two
    // buttons (KNOT-020j).
    avatar: {
      marginLeft: spacing.xs,
      marginRight: spacing.xs,
    },
    // One feed card: a full-width white block held apart from the next by a band of
    // the feed's separator surface. The band — not a rule drawn on the card, and not
    // an inset that would turn the feed into floating cards — is what makes two
    // adjacent entries read as separate things, the way a mature social feed does
    // (KNOT-023b, KNOT-032, KNOT-032-fix1). It is deliberately 8pt and square-edged:
    // enough grey to separate, not enough to open a gutter.
    card: {
      backgroundColor: colors.bg.canvas,
      marginBottom: spacing.sm,
    },
    // The page behind the cards. It is the same surface the inter-card band shows,
    // so the band and the page never differ; `bg.sunken` was too close to white to
    // read as a separator at 8pt (KNOT-032-fix1).
    container: {
      backgroundColor: colors.bg.separator,
      flex: 1,
    },
    // The band between the tab row and the first story, and between every two
    // stories: one surface, one height, full bleed. The top bar paints the canvas,
    // so this band is visible against it exactly as it is against two cards.
    feedDivider: {
      backgroundColor: colors.bg.separator,
      height: spacing.sm,
    },
    error: {
      color: colors.state.danger,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.md,
      textAlign: 'center',
    },
    footerSpinner: {
      marginVertical: spacing.xl,
    },
    headerActions: {
      alignItems: 'center',
      flexDirection: 'row',
    },
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    listContent: {
      paddingBottom: spacing.xxxl,
    },
    // The Thread sits flush under the tab row: the list content starts there, so
    // the line reads as the refresh indicator rather than as part of a card.
    refreshingThread: {
      marginBottom: spacing.xs,
    },
    // One row of the post options sheet: a leading icon and the label, at the 44pt
    // minimum touch size. Rows are separated by spacing alone — no drawn dividers.
    option: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.md,
      minHeight: 44,
      paddingVertical: spacing.md,
    },
    optionLabel: {
      color: colors.text.primary,
      flex: 1,
      fontWeight: fontWeights.bold,
    },
    optionLabelDanger: {
      color: colors.state.danger,
    },
    // The options sheet: rounded top corners, the canvas fill, and the floating-bar
    // shadow (elevation 3, whose offset points up) — the profile sheet's treatment.
    sheet: {
      ...elevation[3],
      backgroundColor: colors.bg.sheet,
      borderTopLeftRadius: radius.xxl,
      borderTopRightRadius: radius.xxl,
      paddingBottom: spacing.xl,
    },
    sheetBackdrop: {
      backgroundColor: colors.scrim.overlay,
      flex: 1,
      justifyContent: 'flex-end',
    },
    sheetBody: {
      paddingHorizontal: spacing.lg,
    },
    // Cancel is a plain full-width row rather than a third option: it acts on the
    // sheet, not on the post.
    sheetCancel: {
      alignItems: 'center',
      borderTopColor: colors.border.hairline,
      borderTopWidth: 1,
      justifyContent: 'center',
      marginTop: spacing.sm,
      minHeight: 48,
      paddingTop: spacing.md,
    },
    sheetCancelText: {
      color: colors.text.secondary,
    },
    sheetHandle: {
      backgroundColor: colors.border.hairline,
      borderRadius: radius.pill,
      height: 4,
      width: 32,
    },
    sheetHandleArea: {
      alignItems: 'center',
      paddingBottom: spacing.sm,
      paddingTop: spacing.sm,
    },
    spinner: {
      marginTop: spacing.xl,
    },
    // One tab, one quarter of the row: the label is centred and single-lined, so it
    // cannot clip. The 2px underline sits below the label, over the row's hairline.
    // The four-column tab. The row draws no rule of its own: the feed's separator
    // band below it does that job (KNOT-020j-fix2).
    tab: {
      alignItems: 'center',
      flex: 1,
      paddingBottom: spacing.sm,
      paddingTop: spacing.md,
      position: 'relative',
    },
    // The selected tab's marker: a 2pt indigo rule along the tab's own width,
    // drawn only for the active tab.
    tabUnderline: {
      backgroundColor: colors.brand.primary,
      bottom: 0,
      height: 2,
      left: 0,
      position: 'absolute',
      right: 0,
    },
    tabLabel: {
      color: colors.text.tertiary,
      textAlign: 'center',
    },
    tabLabelActive: {
      color: colors.text.primary,
    },
    tabs: {
      flexDirection: 'row',
      paddingHorizontal: spacing.lg,
    },
    topBar: {
      backgroundColor: colors.bg.canvas,
      zIndex: 1,
    },
  });

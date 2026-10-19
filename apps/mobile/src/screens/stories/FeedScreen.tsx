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
import { Bell, EyeSlash, Flag, MagnifyingGlass, type Icon } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { notificationsApi } from '../../api/notifications';
import { FEED_PAGE_SIZE, type FeedItem, storiesApi } from '../../api/stories';
import EmptyState from '../../components/EmptyState';
import FeedEntry from '../../components/FeedEntry';
import Header from '../../components/Header';
import Logomark from '../../components/Logomark';
import ReportModal from '../../components/ReportModal';
import TheThread from '../../components/TheThread';
import {
  colors,
  elevation,
  fontWeights,
  layout,
  radius,
  spacing,
  tabularNums,
  typeScale,
} from '../../theme';
import {
  VIEWABILITY_CONFIG,
  type ViewableItemsInfo,
  activeViewableKey,
} from '../../utils/viewability';

/** The logomark's size in the feed header. */
const LOGOMARK_SIZE = 32;

/**
 * The logomark's horizontal nudge, so its left edge lines up with the content
 * beneath it.
 *
 * The header's own gutter is `spacing.sm` (8pt); the feed's entries sit at
 * `layout.screenPaddingFeed` (16pt). The extra 8pt here lifts the mark onto that
 * same column, which is what the home reference draws (its header is
 * `padding: 0 6px 0 16px`). Without it the mark reads as drifting left of every
 * story below it (KNOT-020g-fix13).
 */
const LOGOMARK_INSET = spacing.sm;

/** The header icon size. */
const ICON_SIZE = 24;

/** The text tabs above the feed. They are a local selection until the API grows a filter. */
const FEED_TABS = ['For you', 'Wonder', 'Heritage', 'Nearby'] as const;

type FeedScreenProps = {
  /** The signed-in user's access token, needed to read the unread count and the feed. */
  readonly token: string;
  /** Called when a story is tapped. */
  readonly onOpenStory: (id: string) => void;
  /**
   * Called when an ADAPTATION entry is tapped, with the version's id, so the feed
   * can open the version rather than the story it adapts (KNOT-022).
   */
  readonly onOpenVersion: (versionId: string) => void;
  /** Called when a story's author is tapped, to open their profile. */
  readonly onOpenUserProfile: (userId: string) => void;
  /** Called when the person opens their notifications. */
  readonly onOpenNotifications: () => void;
  /** Called when the person opens search. */
  readonly onOpenSearch: () => void;
};

/**
 * The public story feed, newest first, as white cards on a grey canvas.
 *
 * Each entry is a white card, inset from the screen edges and separated from the
 * next by a band of the grey canvas — the LinkedIn pattern. The separation is the
 * gap itself, not a rule drawn on white, so the cards read as distinct blocks
 * (KNOT-023b, KNOT-032).
 *
 * The layout pins everything but the list: the header and the four text tabs are
 * fixed siblings ABOVE the FlatList, so only the feed entries scroll beneath them
 * (the header is opaque and static — nothing about it is scroll-linked). The tab
 * row is a plain flex row of four equal-width tabs rather than a horizontal
 * scroller, so the labels are centred, never clipped, and never slide sideways.
 * The tabs are a local selection only — the feed endpoint has no pillar filter
 * yet, so they do not change the query (KNOT-020c disclosure).
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
  onOpenStory,
  onOpenVersion,
  onOpenUserProfile,
  onOpenNotifications,
  onOpenSearch,
}: FeedScreenProps): React.ReactElement {
  const [items, setItems] = useState<readonly FeedItem[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [unreadCount, setUnreadCount] = useState(0);
  const [activeTab, setActiveTab] = useState<string>(FEED_TABS[0]);
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

  const loadFirstPage = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const page = await storiesApi.listStories({ limit: FEED_PAGE_SIZE }, token);
      setItems(page.items);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    void loadFirstPage();
  }, [loadFirstPage]);

  /**
   * Reads the bell's badge number.
   *
   * The count is supplementary: a failure leaves the badge as it was rather than
   * showing an error over the feed, because the inbox itself still works.
   */
  const refreshUnreadCount = useCallback(async (): Promise<void> => {
    try {
      const { count } = await notificationsApi.getUnreadCount(token);
      setUnreadCount(count);
    } catch {
      // Intentionally ignored: a badge is not worth an error state.
    }
  }, [token]);

  // Reading the badge on mount also covers returning to the feed: leaving the tab
  // or popping an overlay unmounts this screen, so coming back remounts it and
  // runs this effect again.
  useEffect(() => {
    void refreshUnreadCount();
  }, [refreshUnreadCount]);

  /** Pull-to-refresh: reload the first page from the top. */
  const refresh = useCallback(async (): Promise<void> => {
    setRefreshing(true);
    setError(undefined);

    try {
      const page = await storiesApi.listStories({ limit: FEED_PAGE_SIZE }, token);
      setItems(page.items);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setRefreshing(false);
    }
  }, [token]);

  const loadMore = useCallback(async (): Promise<void> => {
    if (nextCursor === '' || loadingMore) {
      return;
    }

    setLoadingMore(true);
    setError(undefined);

    try {
      const page = await storiesApi.listStories(
        { cursor: nextCursor, limit: FEED_PAGE_SIZE },
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
  }, [loadingMore, nextCursor, token]);

  /** Opens the inbox and re-reads the badge, so a tap always shows the current count. */
  const handleOpenNotifications = useCallback((): void => {
    onOpenNotifications();
    void refreshUnreadCount();
  }, [onOpenNotifications, refreshUnreadCount]);

  const bellLabel =
    unreadCount === 0 ? 'Notifications, none unread' : `Notifications, ${unreadCount} unread`;

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
   * Hides one entry from this feed.
   *
   * Hiding is local to the session. There is no hide endpoint, so the entry leaves
   * the list but returns on the next refresh or page load — it is a "not now",
   * not a mute, and the app does not pretend otherwise (KNOT-031).
   */
  const hidePost = useCallback((): void => {
    const hidden = menuItem;
    setMenuItem(undefined);

    if (hidden === undefined) {
      return;
    }

    hideEntry(hidden.id);
  }, [hideEntry, menuItem]);

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
            <View style={styles.logomark}>
              <Logomark size={LOGOMARK_SIZE} />
            </View>
          }
          right={
            <View style={styles.headerActions}>
              <Pressable
                style={styles.iconButton}
                onPress={onOpenSearch}
                accessibilityRole="button"
                accessibilityLabel="Search stories, versions, and questions"
              >
                <MagnifyingGlass color={colors.text.tertiary} size={ICON_SIZE} />
              </Pressable>
              <Pressable
                style={styles.iconButton}
                onPress={handleOpenNotifications}
                accessibilityRole="button"
                accessibilityLabel={bellLabel}
              >
                <Bell color={colors.text.tertiary} size={ICON_SIZE} />
                {unreadCount > 0 ? (
                  <View style={styles.badge}>
                    <Text style={[typeScale.caption, tabularNums, styles.badgeText]}>
                      {unreadCount > 99 ? '99+' : String(unreadCount)}
                    </Text>
                  </View>
                ) : null}
              </Pressable>
            </View>
          }
        />

        {/* Four equal-width tabs. Every label takes `typeScale.button` (15 pt
            semibold), which is the weight the design reference draws all four
            tabs at; the active tab is distinguished by ink and the indigo
            underline rather than by weight, so the row stays confident and even
            (KNOT-020c-fix2). */}
        <View style={styles.tabs}>
          {FEED_TABS.map((label) => {
            const active = label === activeTab;
            return (
              <Pressable
                key={label}
                style={[styles.tab, active ? styles.tabActive : null]}
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
              </Pressable>
            );
          })}
        </View>
      </View>

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
          // The LinkedIn-style card: a white block inset on the grey feed canvas,
          // held apart from its neighbours by that grey band rather than by a rule
          // drawn on the card (KNOT-023b, KNOT-032).
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
              onOpenOptions={() => setMenuItem(item)}
              // The one-tap hide, straight from the card: the same session-local
              // rule the options sheet applies, without the extra sheet (KNOT-032).
              onHide={() => hideEntry(item.id)}
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
        onHide={hidePost}
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
    </View>
  );
}

/**
 * The post options sheet: the actions a reader can take on one feed entry — hide
 * it from this feed, or report it to the moderators.
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

const styles = StyleSheet.create({
  badge: {
    alignItems: 'center',
    backgroundColor: colors.brand.primary,
    borderRadius: radius.pill,
    justifyContent: 'center',
    minWidth: 18,
    paddingHorizontal: spacing.xs,
    position: 'absolute',
    right: 6,
    top: 6,
  },
  badgeText: {
    color: colors.text.onBrand,
  },
  // One feed card: a white block inset from the screen edges and held apart from
  // the next by a band of the grey canvas. The band — not a rule drawn on the
  // card — is what makes two adjacent entries read as separate things, which is
  // the LinkedIn pattern (KNOT-023b, KNOT-032). There is deliberately no corner
  // radius: the media inside is full-bleed, so rounding the card would need
  // `overflow: hidden` and would clip the media's own edges.
  card: {
    backgroundColor: colors.bg.canvas,
    marginBottom: spacing.md,
    marginHorizontal: spacing.sm,
  },
  container: {
    backgroundColor: colors.bg.sunken,
    flex: 1,
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
  // The mark's own inset, on top of the header's 8pt gutter, so it lines up with
  // the feed's 16pt content column (KNOT-020g-fix13).
  logomark: {
    marginLeft: LOGOMARK_INSET,
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
    backgroundColor: colors.bg.canvas,
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
  tab: {
    alignItems: 'center',
    borderBottomColor: colors.border.hairline,
    borderBottomWidth: 1,
    flex: 1,
    paddingBottom: spacing.sm,
    paddingTop: spacing.md,
  },
  tabActive: {
    borderBottomColor: colors.brand.primary,
    borderBottomWidth: 2,
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

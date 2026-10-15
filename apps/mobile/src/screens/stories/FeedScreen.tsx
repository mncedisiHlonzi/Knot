import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { Bell, MagnifyingGlass } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { notificationsApi } from '../../api/notifications';
import { FEED_PAGE_SIZE, type Story, storiesApi } from '../../api/stories';
import EmptyState from '../../components/EmptyState';
import FeedEntry from '../../components/FeedEntry';
import Header from '../../components/Header';
import Logomark from '../../components/Logomark';
import { colors, layout, radius, spacing, tabularNums, typeScale } from '../../theme';

/** The logomark's size in the feed header. */
const LOGOMARK_SIZE = 32;

/** The header icon size. */
const ICON_SIZE = 24;

/** The text tabs above the feed. They are a local selection until the API grows a filter. */
const FEED_TABS = ['For you', 'Wonder', 'Heritage', 'Nearby'] as const;

/**
 * How much of an entry must be on screen before it becomes the active one. At 60%
 * only one entry can normally qualify at a time, which is what keeps playback to a
 * single video (KNOT-020c-fix3).
 */
const VIEWABILITY_CONFIG = { itemVisiblePercentThreshold: 60 };

/** The slice of the viewability callback's payload the feed reads. */
type ViewabilityInfo = {
  readonly viewableItems: ReadonlyArray<{ readonly key: string | number }>;
};

type FeedScreenProps = {
  /** The signed-in user's access token, needed to read the unread count and the feed. */
  readonly token: string;
  /** Called when a story is tapped. */
  readonly onOpenStory: (id: string) => void;
  /** Called when a story's author is tapped, to open their profile. */
  readonly onOpenUserProfile: (userId: string) => void;
  /** Called when the person opens their notifications. */
  readonly onOpenNotifications: () => void;
  /** Called when the person opens search. */
  readonly onOpenSearch: () => void;
};

/**
 * The public story feed, newest first, as a continuous hairline-separated column.
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
 */
export default function FeedScreen({
  token,
  onOpenStory,
  onOpenUserProfile,
  onOpenNotifications,
  onOpenSearch,
}: FeedScreenProps): React.ReactElement {
  const [stories, setStories] = useState<readonly Story[]>([]);
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

  // The viewability config and the callback must keep their identity for the life
  // of the list — FlatList misbehaves if either is re-created — so they are held in
  // refs rather than rebuilt on every render.
  const viewabilityConfig = useRef(VIEWABILITY_CONFIG).current;
  const onViewableItemsChanged = useRef((info: ViewabilityInfo): void => {
    const first = info.viewableItems[0];
    setActiveItemId(first === undefined ? undefined : String(first.key));
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
      setStories(page.stories);
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
      setStories(page.stories);
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
      // repeats a story already on screen.
      setStories((existing) => [...existing, ...page.stories]);
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

  return (
    <View style={styles.container}>
      {/* The fixed top region: the header and the tab row never scroll and never
          fade. Only the FlatList below them scrolls. */}
      <View style={styles.topBar}>
        <Header
          pinned
          left={<Logomark size={LOGOMARK_SIZE} />}
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
        data={stories}
        keyExtractor={(story) => story.id}
        contentContainerStyle={styles.listContent}
        viewabilityConfig={viewabilityConfig}
        onViewableItemsChanged={onViewableItemsChanged}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={refresh}
            tintColor={colors.text.secondary}
          />
        }
        onEndReached={() => {
          void loadMore();
        }}
        onEndReachedThreshold={0.5}
        renderItem={({ item }) => (
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
              title: item.title,
              version_count: item.version_count,
            }}
            isMuted={isMuted}
            isVisible={item.id === activeItemId}
            onToggleMute={toggleMute}
            onPress={() => onOpenStory(item.id)}
            onOpenAuthor={() => onOpenUserProfile(item.author_id)}
          />
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
          ) : error !== undefined && stories.length > 0 ? (
            <Text style={styles.error}>{error}</Text>
          ) : null
        }
      />
    </View>
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
  container: {
    backgroundColor: colors.bg.canvas,
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

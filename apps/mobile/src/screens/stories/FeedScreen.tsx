import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Animated,
  FlatList,
  Pressable,
  RefreshControl,
  ScrollView,
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
import { API_BASE_URL } from '../../config/api';
import { colors, layout, radius, spacing, tabularNums, typeScale } from '../../theme';

/** The logomark's size in the feed header. */
const LOGOMARK_SIZE = 32;

/** The header icon size. */
const ICON_SIZE = 24;

/** The text tabs above the feed. They are a local selection until the API grows a filter. */
const FEED_TABS = ['For you', 'Wonder', 'Heritage', 'Nearby'] as const;

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
 * Resolves a story's feed media preview to a full URL, or null.
 *
 * The feed carries at most one preview item; its `url` is a relative path, so it
 * is joined to the API base. A missing or blank preview resolves to null and the
 * entry draws no image.
 */
function mediaPreviewUrl(story: Story): string | null {
  const first = story.media !== undefined ? story.media[0] : undefined;
  if (first === undefined) {
    return null;
  }
  const path = typeof first.url === 'string' ? first.url.trim() : '';
  return path === '' ? null : `${API_BASE_URL}${path}`;
}

/**
 * The public story feed, newest first, as a continuous hairline-separated column.
 *
 * Paging is keyset-based: each page returns an opaque `next_cursor` that is sent
 * back verbatim to fetch the following page; an empty cursor ends the list. The
 * header and the text tabs sit above the scrolling list; the header fades in the
 * canvas fill and lifts as the list scrolls beneath it. The four text tabs are a
 * local selection only — the feed endpoint has no pillar filter yet, so they do
 * not change the query (KNOT-020c disclosure).
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

  // Drives the header's scroll-linked elevation. Held in a ref so it survives
  // re-renders without being re-created.
  const scrollY = useRef(new Animated.Value(0)).current;

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
      <Header
        scrollY={scrollY}
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

      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        style={styles.tabs}
        contentContainerStyle={styles.tabsContent}
      >
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
              >
                {label}
              </Text>
            </Pressable>
          );
        })}
      </ScrollView>

      <FlatList
        data={stories}
        keyExtractor={(story) => story.id}
        contentContainerStyle={styles.listContent}
        onScroll={Animated.event([{ nativeEvent: { contentOffset: { y: scrollY } } }], {
          useNativeDriver: false,
        })}
        scrollEventThrottle={16}
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
              created_at: item.created_at,
              id: item.id,
              language: item.language,
              media_preview_url: mediaPreviewUrl(item),
              pillar: item.pillar,
              place: item.approximate_location,
              preview: item.body,
              reactions: item.reactions,
              title: item.title,
            }}
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
  tab: {
    borderBottomColor: 'transparent',
    borderBottomWidth: 2,
    paddingBottom: spacing.sm,
    paddingTop: spacing.md,
  },
  tabActive: {
    borderBottomColor: colors.brand.primary,
  },
  tabLabel: {
    color: colors.text.tertiary,
  },
  tabLabelActive: {
    color: colors.text.primary,
  },
  tabs: {
    borderBottomColor: colors.border.hairline,
    borderBottomWidth: 1,
    flexGrow: 0,
  },
  tabsContent: {
    gap: spacing.xl,
    paddingHorizontal: layout.screenPaddingFeed,
  },
});

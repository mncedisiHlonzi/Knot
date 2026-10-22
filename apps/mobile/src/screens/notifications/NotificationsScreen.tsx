import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { ArrowLeft } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { NOTIFICATIONS_PAGE_SIZE, Notification, notificationsApi } from '../../api/notifications';
import Avatar from '../../components/Avatar';
import EmptyState from '../../components/EmptyState';
import Header from '../../components/Header';
import {
  fontWeights,
  layout,
  radius,
  spacing,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../../theme';
import type { ThemeColors } from '../../theme';
import { formatRelativeTime } from '../../utils/time';
import { avatarUri, notificationParts } from './notificationFormat';

type NotificationsScreenProps = {
  /** The signed-in user's access token. Every inbox route requires it. */
  readonly token: string;
  /** Called when a notification is tapped, after it has been marked read. */
  readonly onOpenEntity: (notification: Notification) => void;
  /** Called when the person returns to the feed. */
  readonly onBack: () => void;
};

/**
 * The in-app notification inbox: who acted on your content, newest first.
 *
 * Every row is a Pressable that marks the notification read and then hands it to
 * the caller, which opens what it can. Each entity type resolves over public GETs:
 * a version names its story (`GET /versions/{id}`) and a comment names its version
 * and story (`GET /comments/{id}`). A version tap lands on the story detail, while
 * a comment lands on the comment's thread.
 *
 * Paging is keyset-based, exactly as the feed and a thread are: each page returns
 * an opaque `next_cursor` sent back verbatim to fetch the following page, and an
 * empty cursor means the end.
 *
 * Delivery is in-app only (KNOT-ADR-039): this screen is the whole notification
 * feature, and there is no push channel behind it.
 */
export default function NotificationsScreen({
  token,
  onOpenEntity,
  onBack,
}: NotificationsScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [notifications, setNotifications] = useState<readonly Notification[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [markingAll, setMarkingAll] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const loadFirstPage = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const page = await notificationsApi.listNotifications(token, {
        limit: NOTIFICATIONS_PAGE_SIZE,
      });
      setNotifications(page.notifications);
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

  const refresh = useCallback(async (): Promise<void> => {
    setRefreshing(true);
    setError(undefined);

    try {
      const page = await notificationsApi.listNotifications(token, {
        limit: NOTIFICATIONS_PAGE_SIZE,
      });
      setNotifications(page.notifications);
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
      const page = await notificationsApi.listNotifications(token, {
        cursor: nextCursor,
        limit: NOTIFICATIONS_PAGE_SIZE,
      });
      // Append rather than replace: the cursor is exclusive, so a page never
      // repeats a notification already on screen.
      setNotifications((existing) => [...existing, ...page.notifications]);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoadingMore(false);
    }
  }, [loadingMore, nextCursor, token]);

  /**
   * Marks one notification read, then hands it to the caller.
   *
   * The row is updated first so the unread dot disappears immediately. A failed
   * write is reported but does not block the navigation: the person asked to open
   * something, and a read marker is not worth refusing that over.
   */
  const handleOpen = useCallback(
    async (notification: Notification): Promise<void> => {
      if (!notification.read) {
        setNotifications((existing) =>
          existing.map((item) => (item.id === notification.id ? { ...item, read: true } : item)),
        );

        try {
          await notificationsApi.markRead(notification.id, token);
        } catch (caught) {
          setError(describeError(caught));
        }
      }

      onOpenEntity(notification);
    },
    [onOpenEntity, token],
  );

  const handleMarkAllRead = useCallback(async (): Promise<void> => {
    setMarkingAll(true);
    setError(undefined);

    try {
      await notificationsApi.markAllRead(token);
      // The server reports how many it updated, but the intent is "everything is
      // read", so every row is updated rather than counting down.
      setNotifications((existing) =>
        existing.map((item) => (item.read ? item : { ...item, read: true })),
      );
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setMarkingAll(false);
    }
  }, [token]);

  const hasUnread = notifications.some((notification) => !notification.read);

  return (
    <View style={styles.screen}>
      <Header
        title="Notifications"
        left={
          <Pressable
            style={styles.iconButton}
            onPress={onBack}
            accessibilityRole="button"
            accessibilityLabel="Back"
          >
            <ArrowLeft color={colors.text.primary} size={24} />
          </Pressable>
        }
        right={
          hasUnread ? (
            <Pressable
              style={styles.markAllButton}
              onPress={() => {
                void handleMarkAllRead();
              }}
              disabled={markingAll}
              accessibilityRole="button"
              accessibilityLabel="Mark all as read"
              accessibilityState={{ disabled: markingAll }}
            >
              <Text style={[typeScale.label, styles.markAllLabel]}>
                {markingAll ? 'Marking…' : 'Mark all as read'}
              </Text>
            </Pressable>
          ) : undefined
        }
      />

      <FlatList
        data={notifications}
        keyExtractor={(notification) => notification.id}
        contentContainerStyle={styles.content}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={() => {
              void refresh();
            }}
            tintColor={colors.text.secondary}
          />
        }
        renderItem={({ item }) => {
          const { actor, text } = notificationParts(item);

          return (
            <Pressable
              style={[styles.row, item.read ? null : styles.rowUnread]}
              onPress={() => {
                void handleOpen(item);
              }}
              accessibilityRole="button"
              accessibilityLabel={`${actor} ${text}`}
            >
              <Avatar uri={avatarUri(item.actor?.avatar_url ?? '')} displayName={actor} size="md" />

              <View style={styles.rowBody}>
                <Text style={[typeScale.body, styles.sentence]}>
                  <Text style={styles.actor}>{actor}</Text> {text}
                </Text>
                <Text style={[typeScale.caption, styles.when]}>
                  {formatRelativeTime(item.created_at)}
                </Text>
              </View>

              {item.read ? null : <View style={styles.unreadDot} />}
            </Pressable>
          );
        }}
        ListEmptyComponent={
          loading ? (
            <ActivityIndicator style={styles.spinner} />
          ) : error !== undefined ? (
            <Text style={styles.error}>{error}</Text>
          ) : (
            <EmptyState
              illustrationName="no-notifications"
              headline="No notifications yet."
              body="When someone adapts your story, comments on it, or reacts, it shows up here."
            />
          )
        }
        ListFooterComponent={
          error !== undefined && notifications.length > 0 ? (
            <Text style={styles.error}>{error}</Text>
          ) : nextCursor !== '' ? (
            <Pressable
              style={styles.more}
              onPress={() => {
                void loadMore();
              }}
              disabled={loadingMore}
              accessibilityRole="button"
              accessibilityLabel="Load more notifications"
            >
              {loadingMore ? (
                <ActivityIndicator />
              ) : (
                <Text style={[typeScale.button, styles.moreLabel]}>Load more</Text>
              )}
            </Pressable>
          ) : null
        }
      />
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    // Inline emphasis inside the sentence: the actor's name takes the primary ink
    // and the heavier weight, while the clause stays in the body ink. Only colour
    // and weight are overridden — never the size or the line height — so the
    // paragraph keeps one rhythm; there is no bold variant of `typeScale.body`
    // (KNOT-020f).
    actor: {
      color: colors.text.primary,
      fontWeight: fontWeights.bold,
    },
    content: {
      paddingBottom: spacing.xxxl,
    },
    error: {
      color: colors.state.danger,
      marginVertical: spacing.lg,
      paddingHorizontal: layout.screenPaddingFeed,
      textAlign: 'center',
    },
    // A 44-point tap target for the header's leading icon button.
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    // The header's trailing action: body text in the brand ink, inside a 44-point
    // tap target even though it is only as tall as its label.
    markAllButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'flex-end',
      paddingHorizontal: spacing.xs,
    },
    markAllLabel: {
      color: colors.brand.primary,
    },
    more: {
      alignItems: 'center',
      paddingVertical: spacing.lg,
    },
    moreLabel: {
      color: colors.brand.primary,
    },
    // One inbox row: the actor's avatar, the sentence, and when it happened, on a
    // hairline. An unread row carries the paper tint and an indigo dot; a read row
    // is the bare canvas with neither.
    row: {
      alignItems: 'flex-start',
      borderBottomColor: colors.border.hairline,
      borderBottomWidth: 1,
      flexDirection: 'row',
      gap: spacing.md,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.md,
    },
    rowBody: {
      flex: 1,
      gap: spacing.xs,
    },
    rowUnread: {
      backgroundColor: colors.bg.paper,
    },
    screen: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    sentence: {
      color: colors.text.secondary,
    },
    spinner: {
      marginTop: spacing.xl,
    },
    unreadDot: {
      backgroundColor: colors.brand.primary,
      borderRadius: radius.pill,
      height: 8,
      marginTop: spacing.sm,
      width: 8,
    },
    when: {
      color: colors.text.tertiary,
    },
  });

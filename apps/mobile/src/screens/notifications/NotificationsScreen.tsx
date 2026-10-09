import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Image,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  View,
} from 'react-native';

import { describeError } from '../../api/client';
import {
  NOTIFICATIONS_PAGE_SIZE,
  Notification,
  NotificationEntityType,
  notificationsApi,
} from '../../api/notifications';
import RootedBadge from '../../components/RootedBadge';
import { API_BASE_URL } from '../../config/api';
import { colors, fontSizes, fontWeights, lineHeights, radius, spacing } from '../../theme';
import { formatRelativeTime } from '../../utils/time';

type NotificationsScreenProps = {
  /** The signed-in user's access token. Every inbox route requires it. */
  readonly token: string;
  /** Called when a notification is tapped, after it has been marked read. */
  readonly onOpenEntity: (notification: Notification) => void;
  /** Called when the person returns to the feed. */
  readonly onBack: () => void;
};

/**
 * The entity types a notification can open.
 *
 * Only a version can be opened from the id the inbox carries: the API serves
 * `GET /versions/{id}`, which also names the story, so a tap can land on the story
 * behind it. A comment or a bridge id does not resolve to a version on the client
 * today (there is no "get comment by id" route), so those rows are marked read and
 * listed without a chevron rather than pretending to navigate.
 */
const OPENABLE_ENTITY_TYPES: readonly NotificationEntityType[] = ['version'];

/** Builds the absolute URL for an actor's avatar, or undefined when they have none. */
function avatarSource(avatarUrl: string): { uri: string } | undefined {
  return avatarUrl === '' ? undefined : { uri: `${API_BASE_URL}${avatarUrl}` };
}

/** The initials shown when an actor has no avatar, e.g. "Ada Lovelace" -> "AL". */
function initials(displayName: string): string {
  const words = displayName
    .trim()
    .split(/\s+/)
    .filter((word) => word !== '');
  if (words.length === 0) {
    return '?';
  }

  const first = words[0]?.charAt(0) ?? '';
  const last = words.length > 1 ? (words[words.length - 1]?.charAt(0) ?? '') : '';

  return (first + last).toUpperCase();
}

/**
 * Describes a notification in one line, in the second person, so the inbox reads
 * as a sentence about the person reading it.
 *
 * An actor the server could not resolve is named "Someone" rather than being
 * hidden, so a row never loses its meaning when an account is deleted.
 */
function describeNotification(notification: Notification): string {
  const actor = notification.actor?.display_name ?? 'Someone';

  switch (notification.event_type) {
    case 'version.created':
      return `${actor} adapted your version.`;
    case 'comment.created':
      return `${actor} commented on your version.`;
    case 'bridge.created':
      return `${actor} bridged your comment into another language.`;
    default:
      return `${actor} did something on your content.`;
  }
}

/**
 * One actor's avatar, falling back to initials on a coloured circle.
 *
 * The avatar is served by the Knot API, not by object storage, so the URL is the
 * API base plus the path the server returned.
 */
function ActorAvatar({
  displayName,
  avatarUrl,
}: {
  displayName: string;
  avatarUrl: string;
}): React.ReactElement {
  const source = avatarSource(avatarUrl);

  if (source === undefined) {
    return (
      <View style={styles.avatarFallback}>
        <Text style={styles.avatarInitials}>{initials(displayName)}</Text>
      </View>
    );
  }

  return (
    <Image
      source={source}
      style={styles.avatar}
      accessibilityLabel={`${displayName}'s profile picture`}
    />
  );
}

/**
 * The in-app notification inbox: who acted on your content, newest first.
 *
 * Paging is keyset-based, exactly as the feed and a thread are: each page returns
 * an opaque `next_cursor` sent back verbatim to fetch the following page, and an
 * empty cursor means the end.
 *
 * Delivery is in-app only (KNOT-ADR-039): this screen is the whole notification
 * feature, and there is no push channel behind it. Tapping a row marks it read and
 * then hands it to the caller, which opens what it can.
 */
export default function NotificationsScreen({
  token,
  onOpenEntity,
  onBack,
}: NotificationsScreenProps): React.ReactElement {
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
    <FlatList
      data={notifications}
      keyExtractor={(notification) => notification.id}
      contentContainerStyle={styles.content}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={refresh}
          tintColor={colors.text.secondary}
        />
      }
      ListHeaderComponent={
        <View>
          <View style={styles.header}>
            <Pressable style={styles.backButton} onPress={onBack}>
              <Text style={styles.backButtonText}>Back</Text>
            </Pressable>
            <Text style={styles.title}>Notifications</Text>
          </View>

          {hasUnread ? (
            <Pressable
              style={[styles.markAllButton, markingAll ? styles.buttonDisabled : null]}
              onPress={handleMarkAllRead}
              disabled={markingAll}
            >
              <Text style={styles.markAllButtonText}>
                {markingAll ? 'Marking…' : 'Mark all as read'}
              </Text>
            </Pressable>
          ) : null}

          {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}
          {loading ? <ActivityIndicator style={styles.spinner} /> : null}
          {!loading && notifications.length === 0 && error === undefined ? (
            <Text style={styles.empty}>No notifications yet.</Text>
          ) : null}
        </View>
      }
      renderItem={({ item }) => {
        const displayName = item.actor?.display_name ?? 'Someone';
        const openable = OPENABLE_ENTITY_TYPES.includes(item.entity_type);

        return (
          <Pressable
            style={[styles.card, item.read ? null : styles.unreadCard]}
            onPress={() => {
              void handleOpen(item);
            }}
          >
            <ActorAvatar displayName={displayName} avatarUrl={item.actor?.avatar_url ?? ''} />

            <View style={styles.cardBody}>
              <Text style={styles.cardText}>{describeNotification(item)}</Text>
              <Text style={styles.cardMeta}>
                {formatRelativeTime(item.created_at)}
                {openable ? ' · tap to open' : ''}
              </Text>
              {item.actor?.author_rooted ? (
                <View style={styles.rootedBadge}>
                  <RootedBadge
                    place={item.actor.author_rooted.place}
                    durationBucket={item.actor.author_rooted.duration_bucket}
                  />
                </View>
              ) : null}
            </View>

            {item.read ? null : <View style={styles.unreadDot} />}
          </Pressable>
        );
      }}
      ListFooterComponent={
        nextCursor !== '' ? (
          <Pressable
            style={[styles.loadMoreButton, loadingMore ? styles.buttonDisabled : null]}
            onPress={loadMore}
            disabled={loadingMore}
          >
            <Text style={styles.loadMoreButtonText}>{loadingMore ? 'Loading…' : 'Load more'}</Text>
          </Pressable>
        ) : null
      }
    />
  );
}

const styles = StyleSheet.create({
  avatar: {
    backgroundColor: colors.bg.secondary,
    borderRadius: radius.pill,
    height: 40,
    width: 40,
  },
  avatarFallback: {
    alignItems: 'center',
    backgroundColor: colors.brand.purple,
    borderRadius: radius.pill,
    height: 40,
    justifyContent: 'center',
    width: 40,
  },
  avatarInitials: {
    color: colors.text.primary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.semiBold,
  },
  backButton: {
    marginRight: spacing.lg,
    paddingVertical: spacing.sm,
  },
  backButtonText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  card: {
    alignItems: 'flex-start',
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.subtle,
    borderRadius: radius.md,
    borderWidth: 1,
    flexDirection: 'row',
    gap: spacing.md,
    marginTop: spacing.md,
    padding: spacing.lg,
  },
  cardBody: {
    flexGrow: 1,
  },
  cardMeta: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: spacing.xs,
  },
  cardText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    lineHeight: lineHeights.base,
  },
  content: {
    backgroundColor: colors.bg.primary,
    padding: spacing.xl,
    paddingBottom: spacing['3xl'],
  },
  empty: {
    color: colors.text.secondary,
    fontSize: fontSizes.base,
    marginTop: spacing.lg,
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.base,
    marginTop: spacing.lg,
  },
  header: {
    alignItems: 'center',
    flexDirection: 'row',
  },
  loadMoreButton: {
    alignItems: 'center',
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.lg,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
  },
  loadMoreButtonText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  markAllButton: {
    alignSelf: 'flex-start',
    marginTop: spacing.lg,
    paddingVertical: spacing.sm,
  },
  markAllButtonText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  rootedBadge: {
    marginTop: spacing.sm,
  },
  spinner: {
    marginTop: spacing.xl,
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
  },
  unreadCard: {
    borderColor: colors.brand.purple,
  },
  unreadDot: {
    backgroundColor: colors.brand.purple,
    borderRadius: radius.pill,
    height: 10,
    marginTop: spacing.sm,
    width: 10,
  },
});

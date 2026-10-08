import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, FlatList, Pressable, StyleSheet, Text, View } from 'react-native';

import { describeError } from '../../api/client';
import { FEED_PAGE_SIZE, Story, storiesApi } from '../../api/stories';
import { colors, fontSizes, fontWeights, radius, spacing } from '../../theme';

type FeedScreenProps = {
  /** The signed-in account's email, shown so the session is obvious. */
  readonly email: string;
  /** Called when a story is tapped. */
  readonly onOpenStory: (id: string) => void;
  /** Called when the person wants to publish a story. */
  readonly onCreateStory: () => void;
  /** Called when the person signs out. */
  readonly onSignOut: () => void;
};

/**
 * Renders a story's timestamp as a short, locale-independent date.
 *
 * An unparseable timestamp is shown verbatim rather than as "Invalid Date", so a
 * server-side format change is visible instead of confusing.
 */
function formatDate(value: string): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toISOString().slice(0, 10);
}

/**
 * The public story feed, newest first.
 *
 * Paging is keyset-based: each page returns an opaque `next_cursor` that is sent
 * back verbatim to fetch the following page. An empty cursor means the end of
 * the feed, which is what stops the list asking for more.
 */
export default function FeedScreen({
  email,
  onOpenStory,
  onCreateStory,
  onSignOut,
}: FeedScreenProps): React.ReactElement {
  const [stories, setStories] = useState<readonly Story[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const loadFirstPage = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const page = await storiesApi.listStories({ limit: FEED_PAGE_SIZE });
      setStories(page.stories);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadFirstPage();
  }, [loadFirstPage]);

  const loadMore = useCallback(async (): Promise<void> => {
    if (nextCursor === '' || loadingMore) {
      return;
    }

    setLoadingMore(true);
    setError(undefined);

    try {
      const page = await storiesApi.listStories({ cursor: nextCursor, limit: FEED_PAGE_SIZE });
      // Append rather than replace: the cursor is exclusive, so a page never
      // repeats a story already on screen.
      setStories((existing) => [...existing, ...page.stories]);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoadingMore(false);
    }
  }, [loadingMore, nextCursor]);

  return (
    <FlatList
      data={stories}
      keyExtractor={(story) => story.id}
      contentContainerStyle={styles.content}
      ListHeaderComponent={
        <View>
          <Text style={styles.title}>Knot</Text>
          <Text style={styles.session}>Signed in as {email}</Text>

          <View style={styles.actions}>
            <Pressable style={styles.primaryButton} onPress={onCreateStory}>
              <Text style={styles.primaryButtonText}>Tell a story</Text>
            </Pressable>
            <Pressable style={styles.secondaryButton} onPress={onSignOut}>
              <Text style={styles.secondaryButtonText}>Sign out</Text>
            </Pressable>
          </View>

          <Text style={styles.sectionTitle}>Stories</Text>

          {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}
          {loading ? <ActivityIndicator style={styles.spinner} /> : null}
          {!loading && stories.length === 0 && error === undefined ? (
            <Text style={styles.empty}>No stories yet. Be the first to tell one.</Text>
          ) : null}
        </View>
      }
      renderItem={({ item }) => (
        <Pressable style={styles.card} onPress={() => onOpenStory(item.id)}>
          <Text style={styles.cardTitle}>{item.title}</Text>
          <Text style={styles.cardMeta}>
            {item.pillar} · {item.language} · {formatDate(item.created_at)}
            {item.sensitive ? ' · sensitive' : ''}
          </Text>
          {item.approximate_location !== '' ? (
            <Text style={styles.cardMeta}>{item.approximate_location}</Text>
          ) : null}
        </Pressable>
      )}
      ListFooterComponent={
        nextCursor !== '' ? (
          <Pressable
            style={[styles.secondaryButton, loadingMore ? styles.buttonDisabled : null]}
            onPress={loadMore}
            disabled={loadingMore}
          >
            <Text style={styles.secondaryButtonText}>{loadingMore ? 'Loading…' : 'Load more'}</Text>
          </Pressable>
        ) : null
      }
    />
  );
}

const styles = StyleSheet.create({
  actions: {
    flexDirection: 'row',
    gap: spacing.md,
    marginTop: spacing.lg,
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  card: {
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.md,
    padding: spacing.lg,
  },
  cardMeta: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: spacing.xs,
  },
  cardTitle: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    fontWeight: fontWeights.semiBold,
  },
  content: {
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
  primaryButton: {
    alignItems: 'center',
    backgroundColor: colors.brand.purple,
    borderRadius: radius.md,
    flexGrow: 1,
    paddingVertical: spacing.md,
  },
  primaryButtonText: {
    color: colors.text.inverse,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  secondaryButton: {
    alignItems: 'center',
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.md,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
  },
  secondaryButtonText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  sectionTitle: {
    color: colors.text.primary,
    fontSize: fontSizes.lg,
    fontWeight: fontWeights.bold,
    marginTop: 28,
  },
  session: {
    color: colors.text.secondary,
    fontSize: fontSizes.base,
    marginTop: spacing.xs,
  },
  spinner: {
    marginTop: spacing.xl,
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
  },
});

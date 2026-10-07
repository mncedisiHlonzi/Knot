import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, FlatList, Pressable, StyleSheet, Text, View } from 'react-native';

import { describeError } from '../../api/client';
import { FEED_PAGE_SIZE, Story, storiesApi } from '../../api/stories';

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
    gap: 12,
    marginTop: 16,
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  card: {
    borderColor: '#d0d7de',
    borderRadius: 8,
    borderWidth: 1,
    marginTop: 12,
    padding: 16,
  },
  cardMeta: {
    color: '#57606a',
    fontSize: 13,
    marginTop: 4,
  },
  cardTitle: {
    color: '#24292f',
    fontSize: 17,
    fontWeight: '600',
  },
  content: {
    padding: 24,
    paddingBottom: 48,
  },
  empty: {
    color: '#57606a',
    fontSize: 15,
    marginTop: 16,
  },
  error: {
    color: '#b3261e',
    fontSize: 14,
    marginTop: 16,
  },
  primaryButton: {
    alignItems: 'center',
    backgroundColor: '#1f6feb',
    borderRadius: 8,
    flexGrow: 1,
    paddingVertical: 12,
  },
  primaryButtonText: {
    color: '#ffffff',
    fontSize: 15,
    fontWeight: '600',
  },
  secondaryButton: {
    alignItems: 'center',
    borderColor: '#d0d7de',
    borderRadius: 8,
    borderWidth: 1,
    marginTop: 12,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  secondaryButtonText: {
    color: '#24292f',
    fontSize: 15,
    fontWeight: '600',
  },
  sectionTitle: {
    color: '#24292f',
    fontSize: 20,
    fontWeight: '700',
    marginTop: 28,
  },
  session: {
    color: '#57606a',
    fontSize: 14,
    marginTop: 4,
  },
  spinner: {
    marginTop: 24,
  },
  title: {
    color: '#24292f',
    fontSize: 26,
    fontWeight: '700',
  },
});

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

import { describeError } from '../../api/client';
import { PLACE_PAGE_SIZE, discoveryApi } from '../../api/discovery';
import { Story } from '../../api/stories';
import { languageName } from '../../data/languages';
import { colors, fontSizes, fontWeights, radius, spacing } from '../../theme';

type PlaceStoriesScreenProps = {
  /** The place whose stories are shown, as it was named by the server. */
  readonly place: string;
  /** Called when a story is tapped. */
  readonly onOpenStory: (id: string) => void;
  /** Called when the person leaves this place. */
  readonly onBack: () => void;
};

/**
 * Renders a story's timestamp as a short, locale-independent date.
 */
function formatDate(value: string): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toISOString().slice(0, 10);
}

/**
 * Every story told at one place, newest first.
 *
 * Paging is the same keyset cursor the feed uses: each page returns an opaque
 * `next_cursor` sent back verbatim for the following page, and an empty cursor
 * means the end. A place with no stories is a normal empty state, not an error —
 * the server answers 200 with an empty list.
 */
export default function PlaceStoriesScreen({
  place,
  onOpenStory,
  onBack,
}: PlaceStoriesScreenProps): React.ReactElement {
  const [stories, setStories] = useState<readonly Story[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const loadFirstPage = useCallback(
    async (mode: 'initial' | 'refresh'): Promise<void> => {
      if (mode === 'initial') {
        setLoading(true);
      } else {
        setRefreshing(true);
      }
      setError(undefined);

      try {
        const page = await discoveryApi.listStoriesAtPlace(place, { limit: PLACE_PAGE_SIZE });
        setStories(page.stories);
        setNextCursor(page.next_cursor);
      } catch (caught) {
        setError(describeError(caught));
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [place],
  );

  useEffect(() => {
    void loadFirstPage('initial');
  }, [loadFirstPage]);

  const loadMore = useCallback(async (): Promise<void> => {
    if (nextCursor === '' || loadingMore) {
      return;
    }

    setLoadingMore(true);
    setError(undefined);

    try {
      const page = await discoveryApi.listStoriesAtPlace(place, {
        cursor: nextCursor,
        limit: PLACE_PAGE_SIZE,
      });
      // Append rather than replace: the cursor is exclusive, so a page never
      // repeats a story already on screen.
      setStories((existing) => [...existing, ...page.stories]);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoadingMore(false);
    }
  }, [loadingMore, nextCursor, place]);

  return (
    <FlatList
      data={stories}
      keyExtractor={(story) => story.id}
      contentContainerStyle={styles.content}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={() => {
            void loadFirstPage('refresh');
          }}
        />
      }
      ListHeaderComponent={
        <View>
          <Pressable style={styles.link} onPress={onBack}>
            <Text style={styles.linkText}>← Back</Text>
          </Pressable>
          <Text style={styles.title}>{place}</Text>

          {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}
          {loading ? <ActivityIndicator style={styles.spinner} /> : null}
          {!loading && stories.length === 0 && error === undefined ? (
            <Text style={styles.empty}>No stories here yet.</Text>
          ) : null}
        </View>
      }
      renderItem={({ item }) => (
        <Pressable style={styles.card} onPress={() => onOpenStory(item.id)}>
          <Text style={styles.cardTitle}>{item.title}</Text>
          <Text style={styles.cardMeta}>
            {item.pillar} · {languageName(item.language)} · {formatDate(item.created_at)}
            {item.sensitive ? ' · sensitive' : ''}
          </Text>
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
  buttonDisabled: {
    opacity: 0.5,
  },
  card: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.subtle,
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
  link: {
    marginBottom: spacing.md,
  },
  linkText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
  },
  secondaryButton: {
    alignItems: 'center',
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.md,
    paddingVertical: spacing.md,
  },
  secondaryButtonText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
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

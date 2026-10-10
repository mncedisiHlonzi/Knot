import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import { describeError } from '../../api/client';
import { INQUIRY_PAGE_SIZE, Inquiry, inquiriesApi } from '../../api/inquiries';
import AuthorLine from '../../components/AuthorLine';
import { colors, fontSizes, fontWeights, lineHeights, radius, spacing } from '../../theme';
import { answerCountLabel, inquiryPlaceLabel } from '../../utils/inquiry';

type InquiryListScreenProps = {
  /** The signed-in user's access token, so a page can carry their own reaction highlights. */
  readonly token: string;
  /** Called when a question is tapped. */
  readonly onOpenInquiry: (inquiryId: string) => void;
  /** Called when the person wants to ask a question. */
  readonly onAsk: () => void;
  /** Called when a question's author is tapped. */
  readonly onOpenUserProfile: (userId: string) => void;
};

/**
 * Curious Inquiries: the open questions, newest first.
 *
 * This is one of the four primary destinations (a tab root), so it has no back
 * link — a tab is a destination, not a pushed screen (KNOT-ADR-044). Asking is
 * reached from here rather than from the Create tab, which stays a story
 * composer (KNOT-ADR-058).
 *
 * The list is public: it reads `GET /inquiries`, which needs no token. The token
 * is passed anyway so a signed-in reader's own reaction highlights travel with the
 * questions, exactly as they do on the feed.
 *
 * An optional place filter narrows the page to one place. It is an exact match on
 * the spelling the asker gave, so the field is a filter to type a place into, not
 * a search to browse.
 */
export default function InquiryListScreen({
  token,
  onOpenInquiry,
  onAsk,
  onOpenUserProfile,
}: InquiryListScreenProps): React.ReactElement {
  const [inquiries, setInquiries] = useState<readonly Inquiry[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  // The place applied to the query, which lags the text field until the person
  // submits it.
  const [place, setPlace] = useState('');
  const [draftPlace, setDraftPlace] = useState('');
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
        const page = await inquiriesApi.listInquiries(
          { limit: INQUIRY_PAGE_SIZE, ...(place === '' ? {} : { place }) },
          token,
        );
        setInquiries(page.inquiries);
        setNextCursor(page.next_cursor);
      } catch (caught) {
        setError(describeError(caught));
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [place, token],
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
      const page = await inquiriesApi.listInquiries(
        {
          cursor: nextCursor,
          limit: INQUIRY_PAGE_SIZE,
          ...(place === '' ? {} : { place }),
        },
        token,
      );
      // Append rather than replace: the cursor is exclusive, so a page never
      // repeats a question already on screen.
      setInquiries((existing) => [...existing, ...page.inquiries]);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoadingMore(false);
    }
  }, [loadingMore, nextCursor, place, token]);

  return (
    <FlatList
      data={inquiries}
      keyExtractor={(inquiry) => inquiry.id}
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
          <View style={styles.header}>
            <Text style={styles.title}>Inquiries</Text>
            <Pressable
              style={styles.askButton}
              onPress={onAsk}
              accessibilityRole="button"
              accessibilityLabel="Ask a question about a place"
            >
              <Text style={styles.askButtonText}>Ask</Text>
            </Pressable>
          </View>
          <Text style={styles.subtitle}>
            Questions people have about places, answered by the people who know them.
          </Text>

          <TextInput
            style={styles.input}
            value={draftPlace}
            onChangeText={setDraftPlace}
            onSubmitEditing={() => setPlace(draftPlace.trim())}
            returnKeyType="search"
            autoCapitalize="words"
            autoCorrect={false}
            placeholder="Filter by place, e.g. Manguzi"
            placeholderTextColor={colors.text.secondary}
          />
          {place !== '' ? (
            <Pressable
              style={styles.clearFilter}
              onPress={() => {
                setDraftPlace('');
                setPlace('');
              }}
            >
              <Text style={styles.linkText}>Clear the {place} filter</Text>
            </Pressable>
          ) : null}

          {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}
          {loading ? <ActivityIndicator style={styles.spinner} /> : null}
          {!loading && inquiries.length === 0 && error === undefined ? (
            <Text style={styles.empty}>
              {place === ''
                ? 'No questions yet. Ask the first one.'
                : `No questions about ${place} yet.`}
            </Text>
          ) : null}
        </View>
      }
      renderItem={({ item }) => (
        <Pressable
          style={styles.card}
          onPress={() => onOpenInquiry(item.id)}
          accessibilityRole="button"
          accessibilityLabel={`Open the question: ${item.title}`}
        >
          <Text style={styles.cardTitle}>{item.title}</Text>
          <Text style={styles.cardBody} numberOfLines={2}>
            {item.body}
          </Text>
          <View style={styles.cardMeta}>
            <Text style={styles.place}>{inquiryPlaceLabel(item.place)}</Text>
            <Text style={styles.answers}>{answerCountLabel(item.answer_count)}</Text>
          </View>
          <AuthorLine
            displayName={item.author_display_name}
            avatarUrl={item.author_avatar_url}
            createdAt={item.created_at}
            rooted={item.author_rooted}
            onPress={() => onOpenUserProfile(item.author_id)}
          />
        </Pressable>
      )}
      ListFooterComponent={
        nextCursor !== '' ? (
          <Pressable
            style={styles.more}
            onPress={() => {
              void loadMore();
            }}
            disabled={loadingMore}
          >
            {loadingMore ? <ActivityIndicator /> : <Text style={styles.linkText}>Load more</Text>}
          </Pressable>
        ) : null
      }
    />
  );
}

const styles = StyleSheet.create({
  answers: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
  },
  askButton: {
    backgroundColor: colors.brand.purple,
    borderRadius: radius.pill,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
  },
  askButtonText: {
    color: colors.text.primary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.semiBold,
  },
  card: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.subtle,
    borderRadius: radius.lg,
    borderWidth: 1,
    marginBottom: spacing.md,
    padding: spacing.lg,
  },
  cardBody: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    lineHeight: lineHeights.sm,
    marginBottom: spacing.sm,
  },
  cardMeta: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: spacing.sm,
  },
  cardTitle: {
    color: colors.text.primary,
    fontSize: fontSizes.lg,
    fontWeight: fontWeights.semiBold,
    marginBottom: spacing.xs,
  },
  clearFilter: {
    marginBottom: spacing.md,
  },
  content: {
    padding: spacing.lg,
  },
  empty: {
    color: colors.text.secondary,
    fontSize: fontSizes.md,
    paddingVertical: spacing.xl,
    textAlign: 'center',
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.sm,
    marginBottom: spacing.md,
  },
  header: {
    alignItems: 'center',
    flexDirection: 'row',
    justifyContent: 'space-between',
  },
  input: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    color: colors.text.primary,
    fontSize: fontSizes.md,
    marginBottom: spacing.sm,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  linkText: {
    color: colors.text.brand,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.medium,
  },
  more: {
    alignItems: 'center',
    paddingVertical: spacing.lg,
  },
  place: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.medium,
  },
  spinner: {
    marginVertical: spacing.lg,
  },
  subtitle: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    lineHeight: lineHeights.sm,
    marginBottom: spacing.lg,
    marginTop: spacing.xs,
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes['2xl'],
    fontWeight: fontWeights.bold,
  },
});

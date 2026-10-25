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
import { INQUIRY_PAGE_SIZE, Inquiry, inquiriesApi } from '../../api/inquiries';
import AuthorLine from '../../components/AuthorLine';
import Button from '../../components/Button';
import Chip from '../../components/Chip';
import EmptyState from '../../components/EmptyState';
import Header from '../../components/Header';
import Input from '../../components/Input';
import { layout, spacing, typeScale, useThemedStyles, type ThemeColors } from '../../theme';
import { answerCountLabel } from '../../utils/inquiry';

/**
 * "INQUIRY · OAXACA, MEXICO" — the overline naming what the row is and where. A
 * question that names no place reads simply "Inquiry".
 */
function inquiryOverline(inquiry: Inquiry): string {
  const place = typeof inquiry.place === 'string' ? inquiry.place.trim() : '';
  const country = typeof inquiry.place_country === 'string' ? inquiry.place_country.trim() : '';
  const where = [place, country].filter((part) => part !== '').join(', ');
  return where === '' ? 'Inquiry' : `Inquiry · ${where}`;
}

/** "6 answers · eng" — the answer tally and the language the question is written in. */
function inquiryMetaLabel(inquiry: Inquiry): string {
  const count = answerCountLabel(inquiry.answer_count);
  const language = typeof inquiry.language === 'string' ? inquiry.language.trim() : '';
  return language === '' ? count : `${count} · ${language}`;
}

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
  const styles = useThemedStyles(createStyles);
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

  const draftTrimmed = draftPlace.trim();
  // A filter the person has typed but not yet applied, so it is offered as a tap
  // rather than fired on every keystroke.
  const filterPending = draftTrimmed !== '' && draftTrimmed !== place;

  return (
    <View style={styles.screen}>
      <Header title="Inquiries" />

      {/* The screen's primary action, on its own line under the header rather
          than as a text link in the header's corner. It is a full-width primary
          `Button`, so asking a question is the loudest thing on the screen — the
          one thing a reader came here to do (KNOT-020g-fix13). The header keeps
          only the centred title, which is where the reference puts it. */}
      <View style={styles.askBar}>
        <Button variant="primary" fullWidth label="Ask a question" onPress={onAsk} />
      </View>

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
            <Text style={[typeScale.bodyS, styles.subtitle]}>
              Questions people have about places, answered by the people who know them.
            </Text>

            <View style={styles.filter}>
              <Input
                label="Place"
                value={draftPlace}
                onChangeText={setDraftPlace}
                autoCapitalize="words"
                returnKeyType="search"
                accessibilityLabel="Filter the list by place"
                placeholder="Filter by place, e.g. Manguzi"
              />
              {filterPending || place !== '' ? (
                <View style={styles.filterChips}>
                  {filterPending ? (
                    <Chip
                      label="Apply filter"
                      variant="outline"
                      selected
                      onPress={() => setPlace(draftTrimmed)}
                    />
                  ) : null}
                  {place !== '' ? (
                    <Chip
                      label={`Clear the ${place} filter`}
                      variant="outline"
                      onPress={() => {
                        setDraftPlace('');
                        setPlace('');
                      }}
                    />
                  ) : null}
                </View>
              ) : null}
            </View>

            {error !== undefined ? (
              <Text style={[typeScale.bodyS, styles.error]}>{error}</Text>
            ) : null}
            {loading ? <ActivityIndicator style={styles.spinner} /> : null}
          </View>
        }
        renderItem={({ item }) => (
          <Pressable
            style={styles.row}
            onPress={() => onOpenInquiry(item.id)}
            accessibilityRole="button"
            accessibilityLabel={`Open the question: ${item.title}`}
          >
            <Text style={[typeScale.overline, styles.rowOverline]} numberOfLines={1}>
              {inquiryOverline(item)}
            </Text>
            <Text style={[typeScale.title, styles.rowTitle]} numberOfLines={3}>
              {item.title}
            </Text>
            {item.body.trim() === '' ? null : (
              <Text style={[typeScale.body, styles.rowBody]} numberOfLines={2}>
                {item.body}
              </Text>
            )}
            <View style={styles.rowMeta}>
              <View style={styles.rowMetaIdentity}>
                <AuthorLine
                  displayName={item.author_display_name}
                  avatarUrl={item.author_avatar_url}
                  rooted={null}
                  onPress={() => onOpenUserProfile(item.author_id)}
                />
              </View>
              <Text style={[typeScale.caption, styles.rowCount]} numberOfLines={1}>
                {inquiryMetaLabel(item)}
              </Text>
            </View>
          </Pressable>
        )}
        ListEmptyComponent={
          loading || error !== undefined ? null : (
            <EmptyState
              illustrationName="no-inquiries"
              headline={place === '' ? 'No questions yet' : `No questions about ${place} yet`}
              body={place === '' ? 'Ask the first one.' : undefined}
            />
          )
        }
        ListFooterComponent={
          nextCursor !== '' ? (
            <View style={styles.footer}>
              <Button
                variant="ghost"
                label="Load more"
                loading={loadingMore}
                onPress={() => {
                  void loadMore();
                }}
              />
            </View>
          ) : null
        }
      />
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    // The Ask action's band: the screen's gutter, with the list's own spacing below
    // it, so the button reads as chrome above the content rather than as the first
    // list row.
    askBar: {
      paddingBottom: spacing.md,
      paddingHorizontal: layout.screenPaddingFeed,
    },
    content: {
      paddingBottom: spacing.xxxl,
      paddingHorizontal: layout.screenPaddingFeed,
    },
    error: {
      color: colors.state.danger,
      marginTop: spacing.md,
    },
    filter: {
      gap: spacing.sm,
      marginTop: spacing.md,
    },
    filterChips: {
      flexDirection: 'row',
      flexWrap: 'wrap',
      gap: spacing.sm,
    },
    footer: {
      paddingVertical: spacing.lg,
    },
    // One question: the overline, the title, up to two lines of the body, then the
    // asker's attribution and the answer tally, closed by the Feed's 4pt separator
    // band — the same rhythm Profile, Place Stories, Search and the Feed draw
    // (KNOT-020k-fix23). Flat rather than a card, per the design system's canvas
    // rule.
    row: {
      borderBottomColor: colors.bg.separator,
      borderBottomWidth: 4,
      gap: spacing.sm,
      marginHorizontal: -layout.screenPaddingFeed,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.lg,
    },
    rowBody: {
      color: colors.text.secondary,
    },
    rowCount: {
      color: colors.text.secondary,
    },
    rowMeta: {
      alignItems: 'center',
      flexDirection: 'row',
      justifyContent: 'space-between',
      marginTop: spacing.xs,
    },
    rowMetaIdentity: {
      flexShrink: 1,
    },
    rowOverline: {
      color: colors.brand.primary,
    },
    rowTitle: {
      color: colors.text.primary,
    },
    screen: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    spinner: {
      marginVertical: spacing.lg,
    },
    subtitle: {
      color: colors.text.secondary,
      marginTop: spacing.sm,
    },
  });

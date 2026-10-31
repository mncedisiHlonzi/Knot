import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { MapPin } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { INQUIRY_PAGE_SIZE, Inquiry, inquiriesApi } from '../../api/inquiries';
import AuthorLine from '../../components/AuthorLine';
import Button from '../../components/Button';
import Chip from '../../components/Chip';
import EmptyState from '../../components/EmptyState';
import Header from '../../components/Header';
import Input from '../../components/Input';
import {
  layout,
  spacing,
  typeScale,
  useTheme,
  useThemedStyles,
  type ThemeColors,
} from '../../theme';
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

/**
 * What the person already has in mind when they leave this screen for the ask
 * form: the place they typed, the starter phrase they tapped, or both. Nothing is
 * sent from here — the composer opens seeded and editable (KNOT-020k-fix9).
 */
export type InquiryDraftSeed = {
  /** The place the question is about. */
  readonly place?: string;
  /** The opening words to put in the question's title field. */
  readonly title?: string;
};

/** The hero's headline: the screen's reason to exist, in the largest type. */
const HERO_HEADLINE = 'What are you curious about?';

/** The hero's one-sentence explanation of what an inquiry is. */
const HERO_SUBTITLE = 'Ask about a place, a tradition or a word. People rooted there will answer.';

/**
 * The starter phrases drawn as chips under the hero.
 *
 * They are openings, not questions: a tap puts the phrase in the composer's title
 * field and the person finishes the sentence — which is the same thing a friend
 * does when they hand you a beginning (KNOT-020k-fix9).
 */
const STARTERS = ['Why do people…', 'What is the name of…', 'Who taught you…'] as const;

type InquiryListScreenProps = {
  /** The signed-in user's access token, so a page can carry their own reaction highlights. */
  readonly token: string;
  /** Called when a question is tapped. */
  readonly onOpenInquiry: (inquiryId: string) => void;
  /**
   * Called when the person wants to ask a question, optionally carrying the place
   * they typed and-or the starter phrase they tapped into the composer.
   */
  readonly onAsk: (seed?: InquiryDraftSeed) => void;
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
 * a search to browse. The same field is the hero's "Which place?" — one place, two
 * explicit actions: "Ask" starts a question about it, "Show questions about …"
 * narrows the list to it (KNOT-020k-fix9).
 */
export default function InquiryListScreen({
  token,
  onOpenInquiry,
  onAsk,
  onOpenUserProfile,
}: InquiryListScreenProps): React.ReactElement {
  const { colors } = useTheme();
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

  /** Opens the ask form, carrying the place already typed into it. */
  const askAboutPlace = useCallback((): void => {
    onAsk(draftTrimmed === '' ? undefined : { place: draftTrimmed });
  }, [draftTrimmed, onAsk]);

  /**
   * Opens the ask form from a starter phrase.
   *
   * A starter is an opening, not a question, so it lands in the composer's title
   * field where the person can finish it. The place typed above travels with it,
   * because a starter is no more specific than the sentence it begins.
   */
  const startFrom = useCallback(
    (starter: string): void => {
      onAsk({
        title: starter,
        ...(draftTrimmed === '' ? {} : { place: draftTrimmed }),
      });
    },
    [draftTrimmed, onAsk],
  );

  return (
    <View style={styles.screen}>
      <Header title="Inquiries" />

      <FlatList
        data={inquiries}
        keyExtractor={(inquiry) => inquiry.id}
        contentContainerStyle={styles.content}
        keyboardShouldPersistTaps="handled"
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={() => {
              void loadFirstPage('refresh');
            }}
          />
        }
        ListHeaderComponent={
          <View style={styles.hero}>
            {/* The screen opens with the QUESTION rather than with its title: the
                tab bar already says where you are, so the first thing a reader
                meets is the invitation to ask (KNOT-020k-fix9). */}
            <Text style={[typeScale.h1, styles.heroHeadline]}>{HERO_HEADLINE}</Text>
            <Text style={[typeScale.body, styles.heroSubtitle]}>{HERO_SUBTITLE}</Text>

            {/* One field and one action: say which place, then ask. The field
                keeps the app's underline treatment with a pin at its leading edge
                so it reads as a place rather than as a search box. */}
            <View style={styles.askRow}>
              <View style={styles.askField}>
                <Input
                  value={draftPlace}
                  onChangeText={setDraftPlace}
                  autoCapitalize="words"
                  returnKeyType="search"
                  accessibilityLabel="Which place is your question about?"
                  placeholder="Which place?"
                  leadingIcon={<MapPin color={colors.text.tertiary} size={20} />}
                />
              </View>
              <Button variant="primary" label="Ask" onPress={askAboutPlace} />
            </View>

            {/* The starters: three ways in, when the person knows the shape of the
                question but not how to begin it. They scroll sideways rather than
                wrap, so the hero keeps a single line of rhythm at any width. */}
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              style={styles.starters}
              contentContainerStyle={styles.startersContent}
            >
              {STARTERS.map((starter) => (
                <Chip
                  key={starter}
                  label={starter}
                  variant="outline"
                  onPress={() => startFrom(starter)}
                />
              ))}
            </ScrollView>

            {/* Narrowing the list is a second, quieter action on the same field.
                It only appears once there is something to narrow by, so the hero
                at rest is the headline, the field and the starters. */}
            {filterPending || place !== '' ? (
              <View style={styles.filterChips}>
                {filterPending ? (
                  <Chip
                    label={`Show questions about ${draftTrimmed}`}
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
    // The field and its button: one line. The row is bottom-aligned so the button's
    // 48pt box sits on the field's underline rather than floating above it
    // (KNOT-020k-fix9).
    askField: {
      flex: 1,
    },
    askRow: {
      alignItems: 'flex-end',
      flexDirection: 'row',
      gap: spacing.sm,
      marginTop: spacing.lg,
    },
    content: {
      paddingBottom: spacing.xxxl,
      paddingHorizontal: layout.screenPaddingFeed,
    },
    error: {
      color: colors.state.danger,
      marginTop: spacing.md,
    },
    filterChips: {
      flexDirection: 'row',
      flexWrap: 'wrap',
      gap: spacing.sm,
      marginTop: spacing.sm,
    },
    footer: {
      paddingVertical: spacing.lg,
    },
    // The hero: the invitation to ask, then the field, the action and the starters.
    // It is the list's header rather than pinned chrome, so the questions take the
    // screen as soon as the person scrolls (KNOT-020k-fix9).
    hero: {
      paddingBottom: spacing.md,
      paddingTop: spacing.sm,
    },
    heroHeadline: {
      color: colors.text.primary,
    },
    heroSubtitle: {
      color: colors.text.secondary,
      marginTop: spacing.sm,
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
    // The starter chips scroll sideways inside the screen gutter: the scroller pulls
    // back to the canvas edge and the chips re-apply the gutter, so the row bleeds
    // naturally instead of stopping short (KNOT-020k-fix9).
    starters: {
      marginHorizontal: -layout.screenPaddingFeed,
      marginTop: spacing.md,
    },
    startersContent: {
      gap: spacing.sm,
      paddingHorizontal: layout.screenPaddingFeed,
    },
  });

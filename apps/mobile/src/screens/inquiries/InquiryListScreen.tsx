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
import { MagnifyingGlass, MapPin } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { INQUIRY_PAGE_SIZE, Inquiry, inquiriesApi } from '../../api/inquiries';
import AuthorLine from '../../components/AuthorLine';
import Avatar from '../../components/Avatar';
import Button from '../../components/Button';
import Chip from '../../components/Chip';
import EmptyState from '../../components/EmptyState';
import Header from '../../components/Header';
import Input from '../../components/Input';
import { API_BASE_URL } from '../../config/api';
import {
  layout,
  radius,
  spacing,
  typeScale,
  useTheme,
  useThemedStyles,
  type ThemeColors,
} from '../../theme';
import { answerCountLabel, filterInquiries } from '../../utils/inquiry';

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
 * form: the starter phrase they tapped. Nothing is sent from here — the composer
 * opens seeded and editable (KNOT-020k-fix9).
 *
 * `place` is still part of the shape because the ask overlay's payload carries one
 * and another caller may seed it; this screen no longer produces it now that the
 * hero's prompt is a button rather than a field (KNOT-020k-fix13).
 */
export type InquiryDraftSeed = {
  /** The place the question is about. */
  readonly place?: string;
  /** The opening words to put in the question's title field. */
  readonly title?: string;
};

/** The hero's headline: the screen's reason to exist, in the largest type. */
const HERO_HEADLINE = 'What are you curious about?';

/** The header glyph size, matching the Feed's `Search` control (KNOT-020k-fix10). */
const ICON_SIZE = 24;

/** The place pin inside the hero's prompt. */
const PIN_ICON_SIZE = 20;

/**
 * The hero's prompt: an invitation to CREATE, not a search box.
 *
 * It is the settled copy of the whole line, so it is drawn as `numberOfLines={1}`
 * text that truncates rather than as a placeholder a person could type into
 * (KNOT-020k-fix12, KNOT-020k-fix13).
 */
const ASK_PROMPT = 'Ask about a place, a tradition, or a word...';

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
  /**
   * The signed-in person, drawn as the header's avatar. Only the two fields the
   * avatar needs are required, so the screen does not depend on the whole `User`
   * record — the same shape the Feed's header takes (KNOT-020k-fix10).
   */
  readonly viewer: {
    readonly display_name: string;
    /** The API-relative avatar path, or an empty string when there is none. */
    readonly avatar_url: string;
  };
  /** Called when a question is tapped. */
  readonly onOpenInquiry: (inquiryId: string) => void;
  /**
   * Called when the person wants to ask a question, optionally carrying the place
   * they typed and-or the starter phrase they tapped into the composer.
   */
  readonly onAsk: (seed?: InquiryDraftSeed) => void;
  /** Called when a question's author is tapped. */
  readonly onOpenUserProfile: (userId: string) => void;
  /** Called when the header avatar is tapped, to open the signed-in person's profile. */
  readonly onOpenProfile: () => void;
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
 * There is no typed place on this screen. The hero's line is a BUTTON — an
 * outlined soft rectangle holding the pin, the invitation and the Ask action — and
 * both halves of it open the composer, so the hero starts questions rather than
 * collecting text (KNOT-020k-fix13). The list endpoint's `?place=` filter is still
 * supported by the API; nothing on this screen sets it now.
 *
 * The bar's magnifier opens an INLINE search over the questions already on screen
 * rather than the app-wide search overlay: on a tab that lists one kind of thing,
 * a magnifier means "find a question here", and sending the reader to a screen
 * that also searches stories and versions answered a question nobody asked
 * (KNOT-020k-fix11).
 */
export default function InquiryListScreen({
  token,
  viewer,
  onOpenInquiry,
  onAsk,
  onOpenUserProfile,
  onOpenProfile,
}: InquiryListScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [inquiries, setInquiries] = useState<readonly Inquiry[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  // The inline search: the header's magnifier reveals a field that narrows the
  // loaded questions. It is deliberately NOT the global search overlay — from the
  // Inquiries tab a magnifier means "find a question", not "search the whole app"
  // (KNOT-020k-fix11).
  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState('');

  // The header avatar. The API sends an avatar as a relative path, so it is joined
  // to the API base here; a blank path lets `Avatar` draw the initials rather than
  // an empty frame (KNOT-020j, KNOT-020k-fix10).
  const avatarPath = typeof viewer.avatar_url === 'string' ? viewer.avatar_url.trim() : '';
  const avatarUri = avatarPath === '' ? undefined : `${API_BASE_URL}${avatarPath}`;

  const loadFirstPage = useCallback(
    async (mode: 'initial' | 'refresh'): Promise<void> => {
      if (mode === 'initial') {
        setLoading(true);
      } else {
        setRefreshing(true);
      }
      setError(undefined);

      try {
        const page = await inquiriesApi.listInquiries({ limit: INQUIRY_PAGE_SIZE }, token);
        setInquiries(page.inquiries);
        setNextCursor(page.next_cursor);
      } catch (caught) {
        setError(describeError(caught));
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [token],
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
        { cursor: nextCursor, limit: INQUIRY_PAGE_SIZE },
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
  }, [loadingMore, nextCursor, token]);

  // The questions the list draws: the loaded page narrowed by the inline search,
  // so typing filters what is on screen without another request.
  const visibleInquiries = filterInquiries(inquiries, query);
  const searching = query.trim() !== '';

  /**
   * Shows or hides the inline search.
   *
   * Closing it also clears the phrase, so the list never stays narrowed by a
   * field the person can no longer see.
   */
  const toggleSearch = useCallback((): void => {
    if (searchOpen) {
      setQuery('');
    }
    setSearchOpen(!searchOpen);
  }, [searchOpen]);

  /**
   * Opens the ask form.
   *
   * The hero's prompt and the "Ask" button both call this, so the whole line — the
   * pin, the invitation and the action — starts one question. Nothing is carried
   * into the composer: the screen no longer has a field to read a place from
   * (KNOT-020k-fix13).
   */
  const openComposer = useCallback((): void => {
    onAsk();
  }, [onAsk]);

  /**
   * Opens the ask form from a starter phrase.
   *
   * A starter is an opening, not a question, so it lands in the composer's title
   * field where the person can finish it.
   */
  const startFrom = useCallback(
    (starter: string): void => {
      onAsk({ title: starter });
    },
    [onAsk],
  );

  return (
    <View style={styles.screen}>
      {/* The bar carries only the search control and the signed-in person's
          avatar: asking lives in the hero below, where the question starts, so the
          header keeps no action of its own beside them (KNOT-020k-fix10). The
          title reads from the leading edge, on the same gutter as the hero
          (KNOT-020k-fix11). */}
      <Header
        title="Inquiries"
        titleAlign="left"
        right={
          <View style={styles.headerActions}>
            <Pressable
              style={styles.iconButton}
              onPress={toggleSearch}
              accessibilityRole="button"
              accessibilityLabel="Search questions"
              accessibilityState={{ expanded: searchOpen }}
              testID="inquiries-search"
            >
              <MagnifyingGlass color={colors.text.primary} size={ICON_SIZE} weight="bold" />
            </Pressable>
            <View style={styles.avatar}>
              <Avatar
                uri={avatarUri}
                displayName={viewer.display_name}
                size="md"
                onPress={onOpenProfile}
                accessibilityLabel="Open your profile"
                testID="inquiries-profile-avatar"
              />
            </View>
          </View>
        }
      />

      <FlatList
        data={visibleInquiries}
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
            {/* The inline search, revealed by the bar's magnifier. It sits
                immediately under the bar, where the control that opened it lives,
                and narrows the questions already on screen (KNOT-020k-fix11). */}
            {searchOpen ? (
              <View style={styles.searchRow} testID="inquiries-search-row">
                <Input
                  value={query}
                  onChangeText={setQuery}
                  autoCapitalize="none"
                  autoCorrect={false}
                  returnKeyType="search"
                  accessibilityLabel="Search questions"
                  placeholder="Search questions"
                  testID="inquiries-search-input"
                />
              </View>
            ) : null}

            {/* The screen opens with the QUESTION rather than with its title: the
                tab bar already says where you are, so the first thing a reader
                meets is the invitation to ask (KNOT-020k-fix9). */}
            <Text style={[typeScale.h1, styles.heroHeadline]}>{HERO_HEADLINE}</Text>
            <Text style={[typeScale.body, styles.heroSubtitle]}>{HERO_SUBTITLE}</Text>

            {/* One line that reads as a control, not as a search box: a soft
                rounded rectangle with a hairline outline, holding the pin and the
                invitation on the left and the Ask action nested on the right. The
                left half is a BUTTON — tapping it opens the composer exactly like
                Ask does — so nothing on this screen accepts typing
                (KNOT-020k-fix13). */}
            <View style={styles.askLine} testID="inquiries-place-pill">
              <Pressable
                style={styles.askPrompt}
                onPress={openComposer}
                accessibilityRole="button"
                accessibilityLabel="Start a question"
                testID="inquiries-ask-prompt"
              >
                <MapPin color={colors.text.tertiary} size={PIN_ICON_SIZE} />
                <Text style={[typeScale.body, styles.askPromptText]} numberOfLines={1}>
                  {ASK_PROMPT}
                </Text>
              </Pressable>
              <Button
                variant="primary"
                shape="pill"
                label="Ask"
                onPress={openComposer}
                testID="inquiries-ask"
              />
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
          loading || error !== undefined ? null : searching ? (
            <EmptyState
              illustrationName="no-inquiries"
              headline={`No questions match '${query.trim()}'.`}
            />
          ) : (
            <EmptyState
              illustrationName="no-inquiries"
              headline="No questions yet"
              body="Ask the first one."
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
    // The header avatar sits just inside the bar's right gutter, with the small
    // clearance the Feed's header gives it (KNOT-020j, KNOT-020k-fix10).
    avatar: {
      marginLeft: spacing.xs,
      marginRight: spacing.xs,
    },
    // The hero's one line: a soft rounded rectangle with a hairline outline and no
    // fill, holding the prompt on the left and the Ask action nested on the right.
    // An outline rather than the sunken surface it used to be, so it reads as a
    // button that starts something instead of as a search field
    // (KNOT-020k-fix11, KNOT-020k-fix13).
    askLine: {
      alignItems: 'center',
      backgroundColor: 'transparent',
      borderColor: colors.border.hairline,
      borderRadius: radius.lg,
      borderWidth: 1,
      flexDirection: 'row',
      gap: spacing.sm,
      marginTop: spacing.lg,
      paddingLeft: spacing.lg,
      // The button keeps the line's own edge: 4pt of air on the right and above and
      // below it, which leaves the 48pt button inside the 58pt line.
      paddingRight: spacing.xs,
      paddingVertical: spacing.xs,
    },
    // The prompt fills the left side and stretches to the line's inner height, so
    // the whole left area — not just the pin and the words — is one tap target.
    askPrompt: {
      alignItems: 'center',
      alignSelf: 'stretch',
      flex: 1,
      flexDirection: 'row',
      gap: spacing.sm,
    },
    // The invitation is settled copy, not a placeholder, so it truncates on a
    // narrow screen rather than pushing the Ask button off the line.
    askPromptText: {
      color: colors.text.tertiary,
      flexShrink: 1,
    },
    content: {
      paddingBottom: spacing.xxxl,
      paddingHorizontal: layout.screenPaddingFeed,
    },
    error: {
      color: colors.state.danger,
      marginTop: spacing.md,
    },
    footer: {
      paddingVertical: spacing.lg,
    },
    // The header's trailing pair: the search control and the avatar, sharing one row
    // (KNOT-020k-fix10).
    headerActions: {
      alignItems: 'center',
      flexDirection: 'row',
    },
    // A 44pt touch target around the 24pt search glyph, the same box every glyph
    // control in the app draws.
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    // The hero: the invitation to ask, then the prompt line, the starters. It is the
    // list's header rather than pinned chrome, so the questions take the screen as
    // soon as the person scrolls (KNOT-020k-fix9).
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
    // The inline search, revealed under the bar. It keeps the hero's rhythm by
    // taking the gap above the headline rather than adding one below itself.
    searchRow: {
      marginBottom: spacing.md,
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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Modal,
  Pressable,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { ArrowLeft, FunnelSimple, MagnifyingGlass } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import {
  SEARCH_PAGE_SIZE,
  SearchKind,
  SearchParams,
  SearchResult,
  canSearch,
  searchApi,
} from '../../api/search';
import AuthorLine from '../../components/AuthorLine';
import Chip from '../../components/Chip';
import EmptyState from '../../components/EmptyState';
import Header from '../../components/Header';
import Input from '../../components/Input';
import LanguagePicker from '../../components/LanguagePicker';
import PillarTag from '../../components/PillarTag';
import { languageName } from '../../data/languages';
import { layout, radius, spacing, typeScale, useTheme, useThemedStyles } from '../../theme';
import type { ThemeColors } from '../../theme';

/** How long to wait after the last keystroke before searching. */
const DEBOUNCE_MS = 300;

/** How many recent phrases are kept. Session-only: nothing is persisted. */
const MAX_RECENT_SEARCHES = 5;

/** The content-type filter, plus "all". */
type KindFilter = 'all' | SearchKind;

/** The pillar filter, plus "all". */
type PillarFilter = 'all' | 'wonder' | 'heritage';

/** The content-type options, in the order they are offered. */
const KIND_OPTIONS: readonly { readonly value: KindFilter; readonly label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'story', label: 'Stories' },
  { value: 'version', label: 'Versions' },
  { value: 'inquiry', label: 'Inquiries' },
];

/** The pillar options, in the order they are offered. */
const PILLAR_OPTIONS: readonly { readonly value: PillarFilter; readonly label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'wonder', label: 'Wonder' },
  { value: 'heritage', label: 'Heritage' },
];

/** The label shown on a result's kind badge. */
const KIND_LABELS: Readonly<Record<SearchKind, string>> = {
  story: 'Story',
  version: 'Version',
  inquiry: 'Inquiry',
};

type SearchScreenProps = {
  /**
   * The signed-in user's access token, or undefined when signed out. It is sent
   * with every request so the server hides blocked authors' content; an anonymous
   * request sees everything.
   */
  readonly token?: string;
  /** Called when a story result is tapped. */
  readonly onOpenStory: (storyId: string) => void;
  /**
   * Called when a version result is tapped, with the id of the story the version
   * belongs to. A version is shown inside its story's language tree, which lists
   * every telling of the story including this one.
   */
  readonly onOpenVersionTree: (storyId: string) => void;
  /** Called when an inquiry result is tapped. */
  readonly onOpenInquiry: (inquiryId: string) => void;
  /** Called when a result's author is tapped, to open their profile. */
  readonly onOpenUserProfile: (userId: string) => void;
  /** Called when the person leaves search. */
  readonly onBack: () => void;
};

/**
 * A row of single-choice filter chips.
 *
 * It is a plain presentation helper, not a hook-bearing component, so it stays a
 * module-level function and each screen keeps its own filter state. Each chip is
 * the design system's `Chip`, which supplies the selected and unselected fills and
 * labels itself with its own text, so there is no separate group label to thread
 * through here.
 */
function ChipRow({
  options,
  selected,
  onSelect,
}: {
  readonly options: readonly { readonly value: string; readonly label: string }[];
  readonly selected: string;
  readonly onSelect: (value: string) => void;
}): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  return (
    <View style={styles.chipRow}>
      {options.map((option) => {
        const active = option.value === selected;
        return (
          <Chip
            key={option.value}
            label={option.label}
            selected={active}
            variant="outline"
            onPress={() => onSelect(option.value)}
          />
        );
      })}
    </View>
  );
}

/**
 * Search across stories, versions, and inquiries.
 *
 * It is an overlay rather than a tab, reached from the magnifying glass in the
 * feed's header (KNOT-ADR-066). The phrase is debounced so typing does not fire a
 * request per keystroke, and a request is only issued when the server would accept
 * it (a phrase of at least two characters, or a filter) — so clearing the field
 * resets the screen instead of showing a validation error.
 *
 * Recent phrases are kept in memory for the duration of the screen only. They are
 * deliberately not persisted: search history is not a feature of Knot.
 */
export default function SearchScreen({
  token,
  onOpenStory,
  onOpenVersionTree,
  onOpenInquiry,
  onOpenUserProfile,
  onBack,
}: SearchScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  // The text field's contents, and the debounced phrase actually searched for.
  const [draft, setDraft] = useState('');
  const [phrase, setPhrase] = useState('');
  const [kind, setKind] = useState<KindFilter>('all');
  const [pillar, setPillar] = useState<PillarFilter>('all');
  const [language, setLanguage] = useState('');
  const [languageOpen, setLanguageOpen] = useState(false);
  const [place, setPlace] = useState('');

  const [results, setResults] = useState<readonly SearchResult[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [recent, setRecent] = useState<readonly string[]>([]);

  // Debounce the phrase. Every keystroke resets the timer, so a request is only
  // sent once typing pauses.
  useEffect(() => {
    const handle = setTimeout(() => setPhrase(draft.trim()), DEBOUNCE_MS);
    return () => clearTimeout(handle);
  }, [draft]);

  const params = useMemo<SearchParams>(
    () => ({
      ...(phrase === '' ? {} : { q: phrase }),
      ...(kind === 'all' ? {} : { type: kind }),
      ...(pillar === 'all' ? {} : { pillar }),
      ...(language === '' ? {} : { language }),
      ...(place === '' ? {} : { place }),
    }),
    [phrase, kind, pillar, language, place],
  );

  const searchable = canSearch(params);

  // Resets the result area when the request is not one the server would accept.
  const resetResults = useCallback((): void => {
    setResults([]);
    setNextCursor('');
    setError(undefined);
    setLoading(false);
  }, []);

  const loadFirstPage = useCallback(async (): Promise<void> => {
    if (!searchable) {
      resetResults();
      return;
    }

    setLoading(true);
    setError(undefined);

    try {
      const page = await searchApi.search({ ...params, limit: SEARCH_PAGE_SIZE }, token);
      setResults(page.results);
      setNextCursor(page.next_cursor);

      if (phrase !== '') {
        // Most recent first, without duplicates, capped — session-only.
        setRecent((existing) =>
          [phrase, ...existing.filter((item) => item !== phrase)].slice(0, MAX_RECENT_SEARCHES),
        );
      }
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [params, searchable, token, phrase, resetResults]);

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
      const page = await searchApi.search(
        { ...params, cursor: nextCursor, limit: SEARCH_PAGE_SIZE },
        token,
      );
      // Append rather than replace: the cursor is exclusive, so a page never
      // repeats a result already on screen.
      setResults((existing) => [...existing, ...page.results]);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoadingMore(false);
    }
  }, [loadingMore, nextCursor, params, token]);

  const openResult = useCallback(
    (result: SearchResult): void => {
      switch (result.kind) {
        case 'story':
          onOpenStory(result.id);
          return;
        case 'version':
          // A version is shown inside its story's language tree, which needs the
          // story id the server sends alongside the version id.
          if (result.story_id !== null) {
            onOpenVersionTree(result.story_id);
          }
          return;
        case 'inquiry':
          onOpenInquiry(result.id);
          return;
      }
    },
    [onOpenInquiry, onOpenStory, onOpenVersionTree],
  );

  const clearFilters = useCallback((): void => {
    setKind('all');
    setPillar('all');
    setLanguage('');
    setPlace('');
  }, []);

  const filtersActive = kind !== 'all' || pillar !== 'all' || language !== '' || place !== '';

  return (
    <View style={styles.screen}>
      <Header
        title="Search"
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
      />

      <FlatList
        data={results}
        keyExtractor={(result) => `${result.kind}:${result.id}`}
        contentContainerStyle={styles.content}
        keyboardShouldPersistTaps="handled"
        ListHeaderComponent={
          <View>
            {/* The phrase field. Its magnifier rides inside the field's leading
                edge through the design system's `Input` (KNOT-020g), so the
                glyph and the text share one underline. The keyboard behaviour
                is the reference's: no autocapitalisation or autocorrection, and
                a search return key. */}
            <View style={styles.searchField}>
              <Input
                value={draft}
                onChangeText={setDraft}
                autoFocus
                autoCapitalize="none"
                autoCorrect={false}
                returnKeyType="search"
                accessibilityLabel="Search phrase"
                leadingIcon={<MagnifyingGlass color={colors.text.tertiary} size={20} />}
                placeholder="Search stories, versions, and questions"
              />
            </View>

            {draft === '' && recent.length > 0 ? (
              <View style={styles.recentRow}>
                {recent.map((item) => (
                  <Chip key={item} label={item} variant="outline" onPress={() => setDraft(item)} />
                ))}
              </View>
            ) : null}

            <View style={styles.filters}>
              {/* A label for the whole filter group, so the two chip rows below
                  read as facets of one control rather than as loose pills. The
                  funnel is the design's filter glyph and stays in the quiet ink
                  (KNOT-020g-fix13). */}
              <View style={styles.filterHeading}>
                <FunnelSimple color={colors.text.tertiary} size={14} weight="bold" />
                <Text style={[typeScale.overline, styles.filterHeadingLabel]}>Filters</Text>
              </View>

              <ChipRow
                options={KIND_OPTIONS}
                selected={kind}
                onSelect={(value) => setKind(value as KindFilter)}
              />
              <ChipRow
                options={PILLAR_OPTIONS}
                selected={pillar}
                onSelect={(value) => setPillar(value as PillarFilter)}
              />

              <View style={styles.chipRow}>
                <Chip
                  label={language === '' ? 'Language' : languageName(language)}
                  selected={language !== ''}
                  variant="outline"
                  onPress={() => setLanguageOpen(true)}
                />
                {filtersActive ? (
                  <Chip label="Clear filters" variant="outline" onPress={clearFilters} />
                ) : null}
              </View>

              <Input
                label="Place"
                value={place}
                onChangeText={setPlace}
                placeholder="Exact spelling"
              />
            </View>

            {error !== undefined ? (
              <View style={styles.errorBlock}>
                <Text style={styles.error}>{error}</Text>
                <Pressable
                  style={styles.action}
                  onPress={() => {
                    void loadFirstPage();
                  }}
                  accessibilityRole="button"
                  accessibilityLabel="Try the search again"
                >
                  <Text style={[typeScale.button, styles.actionLabel]}>Try again</Text>
                </Pressable>
              </View>
            ) : null}

            {loading ? <ActivityIndicator style={styles.spinner} /> : null}
          </View>
        }
        renderItem={({ item }) => {
          const place = typeof item.place === 'string' ? item.place.trim() : '';

          return (
            <Pressable
              style={styles.result}
              onPress={() => openResult(item)}
              accessibilityRole="button"
              accessibilityLabel={`Open the ${KIND_LABELS[item.kind].toLowerCase()}: ${item.title}`}
            >
              <Text style={[typeScale.overline, styles.resultKind]}>{KIND_LABELS[item.kind]}</Text>
              <Text style={[typeScale.h3, styles.resultTitle]} numberOfLines={2}>
                {item.title}
              </Text>
              {item.preview === '' ? null : (
                <Text style={[typeScale.bodyS, styles.resultPreview]} numberOfLines={2}>
                  {item.preview}
                </Text>
              )}

              <View style={styles.resultAuthor}>
                <AuthorLine
                  displayName={item.author_display_name}
                  avatarUrl={item.author_avatar_url}
                  createdAt={item.created_at}
                  rooted={null}
                  onPress={() => onOpenUserProfile(item.author_id)}
                />
              </View>

              {item.pillar !== null ? (
                <View style={styles.resultTag}>
                  <PillarTag pillar={item.pillar} place={item.place} variant="plain" />
                </View>
              ) : place === '' ? null : (
                <Text style={[typeScale.overline, styles.resultPlace]} numberOfLines={1}>
                  {place}
                </Text>
              )}
            </Pressable>
          );
        }}
        ListEmptyComponent={
          loading ? null : error !== undefined ? null : !searchable ? (
            <EmptyState
              illustrationName="no-results"
              headline="Search Knot"
              body="Find stories, versions, and questions by phrase, place, or lens."
            />
          ) : results.length === 0 ? (
            <EmptyState
              illustrationName="no-results"
              headline={
                phrase === '' ? 'No matches for these filters.' : `No results for '${phrase}'.`
              }
            />
          ) : null
        }
        ListFooterComponent={
          nextCursor !== '' ? (
            <Pressable
              style={styles.more}
              onPress={() => {
                void loadMore();
              }}
              disabled={loadingMore}
              accessibilityRole="button"
              accessibilityLabel="Load more results"
            >
              {loadingMore ? (
                <ActivityIndicator />
              ) : (
                <Text style={[typeScale.button, styles.actionLabel]}>Load more</Text>
              )}
            </Pressable>
          ) : null
        }
      />

      <Modal
        visible={languageOpen}
        animationType="slide"
        transparent
        onRequestClose={() => setLanguageOpen(false)}
      >
        <View style={styles.modalBackdrop}>
          <View style={styles.modalCard}>
            <Text style={[typeScale.h3, styles.modalTitle]}>Filter by language</Text>
            <LanguagePicker
              mode="single"
              selected={language}
              onSelect={(chosen) => {
                setLanguage(chosen.code);
                setLanguageOpen(false);
              }}
              onClear={() => {
                setLanguage('');
                setLanguageOpen(false);
              }}
              placeholder="Search languages"
            />
            <Pressable
              style={styles.action}
              onPress={() => setLanguageOpen(false)}
              accessibilityRole="button"
              accessibilityLabel="Close the language filter"
            >
              <Text style={[typeScale.button, styles.actionLabel]}>Close</Text>
            </Pressable>
          </View>
        </View>
      </Modal>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    // A text action in the brand ink, inside a 44-point tap target even though it
    // is only as tall as its label.
    action: {
      justifyContent: 'center',
      minHeight: 44,
    },
    actionLabel: {
      color: colors.brand.primary,
    },
    chipRow: {
      flexDirection: 'row',
      flexWrap: 'wrap',
      gap: spacing.sm,
    },
    content: {
      paddingBottom: spacing.xxxl,
      paddingHorizontal: layout.screenPaddingFeed,
    },
    error: {
      color: colors.state.danger,
    },
    errorBlock: {
      gap: spacing.sm,
      marginTop: spacing.md,
    },
    filters: {
      gap: spacing.sm,
      marginTop: spacing.md,
    },
    // The filter group's label: the funnel glyph with the overline caption beside
    // it, both in the tertiary ink so the chips below stay the loudest thing here.
    filterHeading: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.xs,
    },
    filterHeadingLabel: {
      color: colors.text.tertiary,
    },
    // A 44-point tap target for the header's leading icon button.
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    modalBackdrop: {
      backgroundColor: colors.scrim.overlay,
      flex: 1,
      justifyContent: 'flex-end',
    },
    modalCard: {
      backgroundColor: colors.bg.canvas,
      borderTopLeftRadius: radius.xxl,
      borderTopRightRadius: radius.xxl,
      padding: spacing.lg,
    },
    modalTitle: {
      color: colors.text.primary,
      marginBottom: spacing.md,
    },
    more: {
      alignItems: 'center',
      paddingVertical: spacing.lg,
    },
    recentRow: {
      flexDirection: 'row',
      flexWrap: 'wrap',
      gap: spacing.sm,
      marginTop: spacing.md,
    },
    // One result: the overline kind, the title, up to two lines of preview, the
    // author line, and the pillar-and-place line, closed by the Feed's 4pt
    // separator band. Flat rather than a card, because the design system keeps a
    // canvas free of cards; the negative inset lets the band run edge to edge while
    // the text stays on the screen's padding (KNOT-020k-fix22).
    result: {
      borderBottomColor: colors.bg.separator,
      borderBottomWidth: 4,
      gap: spacing.xs,
      marginHorizontal: -layout.screenPaddingFeed,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.md,
    },
    resultAuthor: {
      marginTop: spacing.xs,
    },
    resultKind: {
      color: colors.brand.primary,
    },
    resultPlace: {
      color: colors.text.tertiary,
      marginTop: spacing.xs,
    },
    resultPreview: {
      color: colors.text.secondary,
    },
    resultTag: {
      marginTop: spacing.xs,
    },
    resultTitle: {
      color: colors.text.primary,
    },
    screen: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    searchField: {
      marginTop: spacing.md,
    },
    spinner: {
      marginVertical: spacing.lg,
    },
  });

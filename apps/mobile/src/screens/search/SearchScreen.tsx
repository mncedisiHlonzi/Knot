import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Modal,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

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
import LanguagePicker from '../../components/LanguagePicker';
import { languageName } from '../../data/languages';
import { colors, fontSizes, fontWeights, lineHeights, radius, spacing } from '../../theme';

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
 * module-level function and each screen keeps its own filter state.
 */
function ChipRow({
  label,
  options,
  selected,
  onSelect,
}: {
  readonly label: string;
  readonly options: readonly { readonly value: string; readonly label: string }[];
  readonly selected: string;
  readonly onSelect: (value: string) => void;
}): React.ReactElement {
  return (
    <View style={styles.chipRow}>
      {options.map((option) => {
        const active = option.value === selected;
        return (
          <Pressable
            key={option.value}
            style={[styles.chip, active ? styles.chipActive : null]}
            onPress={() => onSelect(option.value)}
            accessibilityRole="button"
            accessibilityState={{ selected: active }}
            accessibilityLabel={`${label}: ${option.label}`}
          >
            <Text style={[styles.chipText, active ? styles.chipTextActive : null]}>
              {option.label}
            </Text>
          </Pressable>
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
      <FlatList
        data={results}
        keyExtractor={(result) => `${result.kind}:${result.id}`}
        contentContainerStyle={styles.content}
        keyboardShouldPersistTaps="handled"
        ListHeaderComponent={
          <View>
            <Pressable style={styles.link} onPress={onBack}>
              <Text style={styles.linkText}>← Back</Text>
            </Pressable>
            <Text style={styles.title}>Search</Text>

            <TextInput
              style={styles.input}
              value={draft}
              onChangeText={setDraft}
              autoFocus
              autoCapitalize="none"
              autoCorrect={false}
              returnKeyType="search"
              placeholder="Search stories, versions, and questions"
              placeholderTextColor={colors.text.secondary}
              accessibilityLabel="Search phrase"
            />

            {draft === '' && recent.length > 0 ? (
              <View style={styles.chipRow}>
                {recent.map((item) => (
                  <Pressable
                    key={item}
                    style={styles.chip}
                    onPress={() => setDraft(item)}
                    accessibilityRole="button"
                    accessibilityLabel={`Search again for ${item}`}
                  >
                    <Text style={styles.chipText}>{item}</Text>
                  </Pressable>
                ))}
              </View>
            ) : null}

            <ChipRow
              label="Content type"
              options={KIND_OPTIONS}
              selected={kind}
              onSelect={(value) => setKind(value as KindFilter)}
            />
            <ChipRow
              label="Pillar"
              options={PILLAR_OPTIONS}
              selected={pillar}
              onSelect={(value) => setPillar(value as PillarFilter)}
            />

            <View style={styles.chipRow}>
              <Pressable
                style={[styles.chip, language !== '' ? styles.chipActive : null]}
                onPress={() => setLanguageOpen(true)}
                accessibilityRole="button"
                accessibilityLabel="Filter by language"
              >
                <Text style={[styles.chipText, language !== '' ? styles.chipTextActive : null]}>
                  {language === '' ? 'Language' : languageName(language)}
                </Text>
              </Pressable>
              {filtersActive ? (
                <Pressable
                  style={styles.chip}
                  onPress={clearFilters}
                  accessibilityRole="button"
                  accessibilityLabel="Clear all filters"
                >
                  <Text style={styles.chipText}>Clear filters</Text>
                </Pressable>
              ) : null}
            </View>

            <TextInput
              style={styles.input}
              value={place}
              onChangeText={setPlace}
              autoCapitalize="words"
              autoCorrect={false}
              returnKeyType="search"
              placeholder="Filter by place, exact spelling"
              placeholderTextColor={colors.text.secondary}
              accessibilityLabel="Place filter"
            />

            {error !== undefined ? (
              <View>
                <Text style={styles.error}>{error}</Text>
                <Pressable
                  style={styles.retry}
                  onPress={() => {
                    void loadFirstPage();
                  }}
                  accessibilityRole="button"
                  accessibilityLabel="Try the search again"
                >
                  <Text style={styles.linkText}>Try again</Text>
                </Pressable>
              </View>
            ) : null}

            {loading ? <ActivityIndicator style={styles.spinner} /> : null}

            {!searchable && !loading ? (
              <Text style={styles.empty}>Search Knot for stories, versions, and questions.</Text>
            ) : null}

            {searchable && !loading && error === undefined && results.length === 0 ? (
              <Text style={styles.empty}>
                {phrase === '' ? 'No matches for these filters.' : `No results for '${phrase}'.`}
              </Text>
            ) : null}
          </View>
        }
        renderItem={({ item }) => (
          <Pressable
            style={styles.card}
            onPress={() => openResult(item)}
            accessibilityRole="button"
            accessibilityLabel={`Open the ${KIND_LABELS[item.kind].toLowerCase()}: ${item.title}`}
          >
            <View style={styles.cardTop}>
              <Text style={styles.kindBadge}>{KIND_LABELS[item.kind]}</Text>
              {item.pillar !== null ? <Text style={styles.metaBadge}>{item.pillar}</Text> : null}
              {item.place !== null && item.place !== '' ? (
                <Text style={styles.metaBadge} numberOfLines={1}>
                  {item.place}
                </Text>
              ) : null}
            </View>

            <Text style={styles.cardTitle}>{item.title}</Text>
            {item.preview !== '' ? (
              <Text style={styles.cardBody} numberOfLines={2}>
                {item.preview}
              </Text>
            ) : null}

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
              accessibilityRole="button"
              accessibilityLabel="Load more results"
            >
              {loadingMore ? <ActivityIndicator /> : <Text style={styles.linkText}>Load more</Text>}
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
            <Text style={styles.modalTitle}>Filter by language</Text>
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
              style={styles.modalClose}
              onPress={() => setLanguageOpen(false)}
              accessibilityRole="button"
              accessibilityLabel="Close the language filter"
            >
              <Text style={styles.linkText}>Close</Text>
            </Pressable>
          </View>
        </View>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
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
  cardTitle: {
    color: colors.text.primary,
    fontSize: fontSizes.lg,
    fontWeight: fontWeights.semiBold,
    marginBottom: spacing.xs,
  },
  cardTop: {
    alignItems: 'center',
    flexDirection: 'row',
    marginBottom: spacing.xs,
  },
  chip: {
    borderColor: colors.border.default,
    borderRadius: radius.pill,
    borderWidth: 1,
    marginBottom: spacing.sm,
    marginRight: spacing.sm,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
  },
  chipActive: {
    backgroundColor: colors.brand.purple,
    borderColor: colors.brand.purple,
  },
  chipRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
  },
  chipText: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
  },
  chipTextActive: {
    color: colors.text.primary,
    fontWeight: fontWeights.semiBold,
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
    marginBottom: spacing.sm,
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
  kindBadge: {
    backgroundColor: colors.bg.secondary,
    borderRadius: radius.sm,
    color: colors.text.brand,
    fontSize: fontSizes.xs,
    fontWeight: fontWeights.semiBold,
    marginRight: spacing.sm,
    overflow: 'hidden',
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
  },
  link: {
    marginBottom: spacing.md,
  },
  linkText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
  },
  metaBadge: {
    color: colors.text.secondary,
    fontSize: fontSizes.xs,
    marginRight: spacing.sm,
  },
  modalBackdrop: {
    backgroundColor: colors.overlay,
    flex: 1,
    justifyContent: 'flex-end',
  },
  modalCard: {
    backgroundColor: colors.bg.primary,
    borderColor: colors.border.subtle,
    borderTopLeftRadius: radius.lg,
    borderTopRightRadius: radius.lg,
    borderWidth: 1,
    padding: spacing.lg,
  },
  modalClose: {
    marginTop: spacing.md,
  },
  modalTitle: {
    color: colors.text.primary,
    fontSize: fontSizes.lg,
    fontWeight: fontWeights.semiBold,
    marginBottom: spacing.md,
  },
  more: {
    alignItems: 'center',
    paddingVertical: spacing.lg,
  },
  retry: {
    marginBottom: spacing.md,
  },
  screen: {
    backgroundColor: colors.bg.primary,
    flex: 1,
  },
  spinner: {
    marginVertical: spacing.lg,
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes['2xl'],
    fontWeight: fontWeights.bold,
    marginBottom: spacing.md,
  },
});

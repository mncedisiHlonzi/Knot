import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Modal,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  View,
  useWindowDimensions,
} from 'react-native';
import {
  ArrowsClockwise,
  EyeSlash,
  Flag,
  MagnifyingGlass,
  PencilSimple,
  Plus,
  type Icon,
} from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { FEED_PAGE_SIZE, type FeedItem, storiesApi } from '../../api/stories';
import Avatar from '../../components/Avatar';
import Button from '../../components/Button';
import Chip from '../../components/Chip';
import ConfirmSheet from '../../components/ConfirmSheet';
import EmptyState from '../../components/EmptyState';
import FeedEntry from '../../components/FeedEntry';
import type { ViewerMedia, ViewerStory } from '../../components/MediaGalleryModal';
import Header from '../../components/Header';
import Logomark from '../../components/Logomark';
import type { Pillar } from '../../components/PillarTag';
import ReportModal from '../../components/ReportModal';
import TheThread from '../../components/TheThread';
import { API_BASE_URL } from '../../config/api';
import {
  elevation,
  fontWeights,
  layout,
  radius,
  spacing,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../../theme';
import type { ThemeColors } from '../../theme';
import {
  VIEWABILITY_CONFIG,
  type ViewableItemsInfo,
  activeViewableKey,
} from '../../utils/viewability';
import {
  PROMPT_PILLARS,
  nextPromptIndex,
  promptAt,
  promptIndexForPillar,
  promptPillarLabel,
  promptPillarName,
  type FeedPrompt,
} from './feedPrompt';

/** The logomark's size in the feed header. */
const LOGOMARK_SIZE = 32;

/** The header icon size. */
const ICON_SIZE = 24;

/**
 * The ordering the server is asked for. `for_you` runs the relevance engine for
 * the signed-in reader; `recent` is the chronological merged feed (KNOT-027).
 */
type FeedType = 'recent' | 'for_you';

/**
 * The text tabs above the feed.
 *
 * `For you` and `Latest` choose the ordering the server applies. The pillar and
 * place tabs are still a local selection: the feed endpoint has no pillar or
 * place filter yet, so tapping them changes the highlight but not the request.
 *
 * TODO: Connect pillar and place filters.
 */
const FEED_TABS = ['For you', 'Latest', 'Wonder', 'Heritage', 'Nearby'] as const;

/** The tabs that change the request. A pillar tab leaves the ordering alone. */
const FEED_TYPE_BY_TAB: Readonly<Record<string, FeedType>> = {
  'For you': 'for_you',
  Latest: 'recent',
};

type FeedScreenProps = {
  /** The signed-in user's access token, needed to read the feed. */
  readonly token: string;
  /**
   * The signed-in person, drawn as the header's avatar. Only the two fields the
   * avatar needs are required, so the screen does not depend on the whole `User`
   * record (KNOT-020j).
   */
  readonly viewer: {
    readonly display_name: string;
    /** The API-relative avatar path, or an empty string when there is none. */
    readonly avatar_url: string;
  };
  /** Called when a story is tapped. */
  readonly onOpenStory: (id: string) => void;
  /**
   * Called when an ADAPTATION entry is tapped, with the version's id, so the feed
   * can open the version rather than the story it adapts (KNOT-022).
   */
  readonly onOpenVersion: (versionId: string) => void;
  /** Called when a story's author is tapped, to open their profile. */
  readonly onOpenUserProfile: (userId: string) => void;
  /** Called when the header avatar is tapped, to open the signed-in person's profile. */
  readonly onOpenProfile: () => void;
  /** Called when an entry's media is tapped, to open the full-screen viewer. */
  readonly onOpenMedia: (
    items: readonly ViewerMedia[],
    index: number,
    story?: ViewerStory | null,
  ) => void;
  /** Called when the person opens search. */
  readonly onOpenSearch: () => void;
  /**
   * Called when the prompt card's call to action is used: the "Write it" button or
   * the prompt itself. Create used to be the bar's elevated tab and then a simple
   * composer row; it is the prompt card now (KNOT-020k-fix6, KNOT-020k-fix7).
   */
  readonly onCreateStory: () => void;
};

/**
 * The public story feed, newest first, as full-width white cards on one continuous
 * column.
 *
 * Each entry spans the whole feed width and is closed by a 4pt band of the
 * separator neutral. The band, not a hairline and not an inset card, is what
 * separates two stories — the rhythm a mature social feed uses (KNOT-023b,
 * KNOT-032, KNOT-020k-fix20).
 *
 * The layout pins everything but the list: the header and the five text tabs are
 * fixed siblings ABOVE the FlatList, so only the feed entries scroll beneath them
 * (the header is opaque and static — nothing about it is scroll-linked). The first
 * row INSIDE the list is the writing prompt, the place a story starts now that
 * Create has left the tab bar (KNOT-020k-fix7). The tab row is a plain flex row of
 * five equal-width tabs rather than a horizontal scroller, so the labels are
 * centred, never clipped, and never slide sideways.
 * `For you` and `Latest` change the request's `feed_type`; the pillar and place
 * tabs stay a local selection until the API grows those filters (KNOT-027).
 *
 * Paging is keyset-based: each page returns an opaque `next_cursor` that is sent
 * back verbatim to fetch the following page; an empty cursor ends the list.
 *
 * The page is a MERGED stream: a story and somebody's adaptation of it are the same
 * kind of entry, and each carries the `kind` that says which it is. The feed renders
 * both through `FeedEntry` -- the card takes a `variant` -- and a tap opens what the
 * entry actually is: the story, or the version (KNOT-022).
 */
export default function FeedScreen({
  token,
  viewer,
  onOpenStory,
  onOpenVersion,
  onOpenUserProfile,
  onOpenProfile,
  onOpenMedia,
  onOpenSearch,
  onCreateStory,
}: FeedScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [items, setItems] = useState<readonly FeedItem[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [activeTab, setActiveTab] = useState<string>(FEED_TABS[0]);
  // The ordering the list is showing. A pillar tab is visual-only, so it keeps
  // whatever ordering was last chosen rather than forcing a re-fetch.
  const feedType: FeedType = FEED_TYPE_BY_TAB[activeTab] ?? 'for_you';
  // The one entry the feed judges to be on screen. Its video plays; every other
  // entry's pauses (KNOT-020c-fix3).
  const [activeItemId, setActiveItemId] = useState<string | undefined>(undefined);
  // One mute setting for the whole feed, muted to begin with: a video that starts
  // itself must never make a sound.
  const [isMuted, setIsMuted] = useState(true);
  // The entry whose options sheet is open, if any (KNOT-031).
  const [menuItem, setMenuItem] = useState<FeedItem | undefined>(undefined);
  // The entry being reported. The report modal owns its own form state, so this
  // only says which entry the form is about.
  const [reportItem, setReportItem] = useState<FeedItem | undefined>(undefined);
  // The entry awaiting a hide confirmation, if any (KNOT-032-fix1). The card's `✕`
  // and the sheet's "Hide this post" row both arm this; nothing leaves the feed
  // until the person answers.
  const [pendingHideId, setPendingHideId] = useState<string | undefined>(undefined);
  // Which prompt the card is showing. One index, wrapped by `feedPrompt`, so a
  // shuffle or a pillar chip needs no copy of the list (KNOT-020k-fix7).
  const [promptIndex, setPromptIndex] = useState(0);
  // The Thread spans the list while a pull-to-refresh is in flight (KNOT-020h).
  const { width: pageWidth } = useWindowDimensions();

  // The viewability config and the callback must keep their identity for the life
  // of the list — FlatList misbehaves if either is re-created — so they are held in
  // refs rather than rebuilt on every render.
  const viewabilityConfig = useRef(VIEWABILITY_CONFIG).current;
  const onViewableItemsChanged = useRef((info: ViewableItemsInfo): void => {
    setActiveItemId(activeViewableKey(info));
  }).current;

  /** Flips the feed-wide mute setting; every video follows it. */
  const toggleMute = useCallback((): void => {
    setIsMuted((current) => !current);
  }, []);

  /** Deals the next prompt — a rotation, so it never repeats the one on screen. */
  const shufflePrompt = useCallback((): void => {
    setPromptIndex((current) => nextPromptIndex(current));
  }, []);

  /** Deals the next prompt of one pillar, which is what a chip tap means. */
  const selectPromptPillar = useCallback((pillar: Pillar): void => {
    setPromptIndex((current) => promptIndexForPillar(current, pillar));
  }, []);

  // The header avatar. The API sends an avatar as a relative path, so it is
  // joined to the API base here; a blank path lets `Avatar` draw the initials
  // rather than an empty frame (KNOT-020j).
  const avatarPath = typeof viewer.avatar_url === 'string' ? viewer.avatar_url.trim() : '';
  const avatarUri = avatarPath === '' ? undefined : `${API_BASE_URL}${avatarPath}`;

  const loadFirstPage = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const page = await storiesApi.listStories({ feedType, limit: FEED_PAGE_SIZE }, token);
      setItems(page.items);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [feedType, token]);

  useEffect(() => {
    void loadFirstPage();
  }, [loadFirstPage]);

  /** Pull-to-refresh: reload the first page from the top. */
  const refresh = useCallback(async (): Promise<void> => {
    setRefreshing(true);
    setError(undefined);

    try {
      const page = await storiesApi.listStories({ feedType, limit: FEED_PAGE_SIZE }, token);
      setItems(page.items);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setRefreshing(false);
    }
  }, [feedType, token]);

  const loadMore = useCallback(async (): Promise<void> => {
    if (nextCursor === '' || loadingMore) {
      return;
    }

    setLoadingMore(true);
    setError(undefined);

    try {
      const page = await storiesApi.listStories(
        { cursor: nextCursor, feedType, limit: FEED_PAGE_SIZE },
        token,
      );
      // Append rather than replace: the cursor is exclusive, so a page never
      // repeats an entry already on screen.
      setItems((existing) => [...existing, ...page.items]);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoadingMore(false);
    }
  }, [feedType, loadingMore, nextCursor, token]);

  /** Closes the options sheet without acting on the entry. */
  const closePostOptions = useCallback((): void => {
    setMenuItem(undefined);
  }, []);

  /**
   * Removes one entry from the list, by id.
   *
   * Both hide paths land here: the options sheet's row, and the card's own
   * one-tap `✕`. Holding the rule in one place keeps them the same rule
   * (KNOT-032).
   */
  const hideEntry = useCallback((id: string): void => {
    setItems((existing) => existing.filter((entry) => entry.id !== id));
  }, []);

  /**
   * Asks before hiding.
   *
   * Both entry points — the card's `✕` and the options sheet's "Hide this post" —
   * only arm the confirmation here. Nothing is removed until the person chooses
   * "Hide story", because a one-tap removal from a scrolling feed is too easy to
   * hit by accident (KNOT-032-fix1).
   */
  const requestHide = useCallback((id: string | undefined): void => {
    // The options sheet, when that is where the request came from, closes as the
    // confirmation opens: two stacked sheets would hide the question behind the
    // choices (KNOT-031).
    setMenuItem(undefined);

    if (id === undefined) {
      return;
    }

    setPendingHideId(id);
  }, []);

  /** Dismisses the confirmation: the story stays exactly where it was. */
  const keepStory = useCallback((): void => {
    setPendingHideId(undefined);
  }, []);

  /**
   * Applies the confirmation.
   *
   * Hiding is local to the session. There is no hide endpoint, so the entry leaves
   * the list but returns on the next refresh or page load — it is a "not now",
   * not a mute, and the app does not pretend otherwise (KNOT-031). This is the
   * only path that removes an entry; `hideEntry` does the removing.
   */
  const confirmHide = useCallback((): void => {
    const target = pendingHideId;
    // Clearing first closes the sheet, and makes a second confirm a no-op even if
    // one somehow arrives (KNOT-032-fix1).
    setPendingHideId(undefined);

    if (target === undefined) {
      return;
    }

    hideEntry(target);
  }, [hideEntry, pendingHideId]);

  /** Hands the entry from the options sheet to the report modal. */
  const reportPost = useCallback((): void => {
    setReportItem(menuItem);
    setMenuItem(undefined);
  }, [menuItem]);

  /**
   * Closes the report modal.
   *
   * The entry stays in place after a report is sent: moderation is asynchronous,
   * so hiding it locally would claim an outcome the app cannot promise.
   */
  const closeReport = useCallback((): void => {
    setReportItem(undefined);
  }, []);

  return (
    <View style={styles.container}>
      {/* The fixed top region: the header and the tab row never scroll and never
          fade. Only the FlatList below them scrolls. */}
      <View style={styles.topBar}>
        <Header
          pinned
          left={
            // The bar's own gutter is `layout.screenPaddingFeed`, the same column
            // the stories sit on, so the mark needs no nudge of its own
            // (KNOT-020g-fix13, KNOT-020j-fix2).
            <Logomark size={LOGOMARK_SIZE} />
          }
          right={
            <View style={styles.headerActions}>
              {/* Create sits first in the action pair, the slot the composer row
                  used to fill: writing is the feed's primary action, so it takes
                  the primary ink while the search glyph beside it stays quiet
                  (KNOT-020k-fix8). It opens the same overlay the prompt card's
                  "Write it" does. */}
              <Pressable
                style={styles.iconButton}
                onPress={onCreateStory}
                accessibilityRole="button"
                accessibilityLabel="Create a story"
                testID="feed-create"
              >
                <Plus color={colors.text.primary} size={ICON_SIZE} weight="bold" />
              </Pressable>
              {/* Search stays a glyph control beside the avatar, the pair the
                  header carries now that the bell has moved to the tab bar
                  (KNOT-020j). */}
              <Pressable
                style={styles.iconButton}
                onPress={onOpenSearch}
                accessibilityRole="button"
                accessibilityLabel="Search stories, versions, and questions"
              >
                <MagnifyingGlass color={colors.text.tertiary} size={ICON_SIZE} />
              </Pressable>
              <View style={styles.avatar}>
                <Avatar
                  uri={avatarUri}
                  displayName={viewer.display_name}
                  size="md"
                  onPress={onOpenProfile}
                  accessibilityLabel="Open your profile"
                  testID="feed-profile-avatar"
                />
              </View>
            </View>
          }
        />

        {/* Five equal-width tabs. Every label takes `typeScale.button` (15 pt
            semibold); the active tab is distinguished by ink and the indigo
            underline rather than by weight, so the row stays confident and even
            (KNOT-020c-fix2). `For you` and `Latest` change the request; the
            pillar and place tabs are visual only (KNOT-027). The row itself draws
            no rule: the 4pt feed band below it separates it from the first story,
            identical to the one between two stories (KNOT-020j-fix2,
            KNOT-020k-fix20). */}
        <View style={styles.tabs}>
          {FEED_TABS.map((label) => {
            const active = label === activeTab;
            return (
              <Pressable
                key={label}
                style={styles.tab}
                onPress={() => setActiveTab(label)}
                accessibilityRole="button"
                accessibilityState={{ selected: active }}
                accessibilityLabel={label}
              >
                <Text
                  style={[typeScale.button, styles.tabLabel, active ? styles.tabLabelActive : null]}
                  numberOfLines={1}
                >
                  {label}
                </Text>
                {/* The selected tab's underline is drawn inside its own tab, at the
                    row's bottom edge, so only the active tab carries a line. */}
                {active ? <View style={styles.tabUnderline} /> : null}
              </Pressable>
            );
          })}
        </View>
      </View>

      {/* The first band: the same full-bleed 4pt line that closes a story card
          (`bg.separator`), so the tab row, every entry and the first story read as
          one rhythm (KNOT-020j-fix2, KNOT-020k-fix20). */}
      <View style={styles.feedDivider} testID="feed-tabs-divider" />

      <FlatList
        data={items}
        keyExtractor={(item) => item.id}
        contentContainerStyle={styles.listContent}
        viewabilityConfig={viewabilityConfig}
        onViewableItemsChanged={onViewableItemsChanged}
        // The Thread is the feed's refresh indicator. It is drawn at the top of
        // the list content rather than inside the RefreshControl, because the
        // platform control owns the pull gesture and cannot host a custom
        // indicator (KNOT-020h). The prompt card sits under it, still at the top
        // of the content, so a refresh line reads above the feed's own first row
        // (KNOT-020k-fix7).
        ListHeaderComponent={
          <View>
            {refreshing ? (
              <View style={styles.refreshingThread} testID="feed-thread">
                <TheThread length={pageWidth} />
              </View>
            ) : null}

            <PromptCard
              prompt={promptAt(promptIndex)}
              onWrite={onCreateStory}
              onShuffle={shufflePrompt}
              onSelectPillar={selectPromptPillar}
            />
          </View>
        }
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={refresh}
            // The Thread is the feed's refresh indicator, so the platform
            // spinner is drawn transparent rather than on top of it
            // (KNOT-020h).
            tintColor="transparent"
            colors={['transparent']}
            progressBackgroundColor={colors.bg.canvas}
          />
        }
        onEndReached={() => {
          void loadMore();
        }}
        onEndReachedThreshold={0.5}
        renderItem={({ item }) => (
          // The card: full width, on the canvas, closed by one 4pt band rather than
          // by a thick gutter or a horizontal inset (KNOT-023b, KNOT-020k-fix20).
          <View style={styles.card}>
            <FeedEntry
              story={{
                author_avatar_url: item.author_avatar_url,
                author_display_name: item.author_display_name,
                author_rooted: item.author_rooted ?? null,
                comment_count: item.comment_count,
                created_at: item.created_at,
                first_media_type: item.first_media_type ?? null,
                first_media_url: item.first_media_url ?? null,
                first_media_width: item.first_media_width ?? null,
                first_media_height: item.first_media_height ?? null,
                id: item.id,
                // The story a version entry adapts, so the viewer's "View Story"
                // has a story to open; a story entry's own id is the story
                // (KNOT-020k-fix2).
                storyId: item.kind === 'version' ? item.story_id : item.id,
                language: item.language,
                language_count: item.language_count,
                media_count: item.media_count,
                pillar: item.pillar,
                place: item.approximate_location,
                preview: item.body,
                preview_truncated: item.preview_truncated,
                reactions: item.reactions,
                recent_interactions: item.recent_interactions,
                // The original author's identity, only on an adaptation; the card
                // heads a version with it. A story entry has no such fields, so
                // the conditional narrows the union (KNOT-023a).
                root_author_avatar_url:
                  item.kind === 'version' ? item.root_author_avatar_url : undefined,
                root_author_display_name:
                  item.kind === 'version' ? item.root_author_display_name : undefined,
                root_author_id: item.kind === 'version' ? item.root_author_id : undefined,
                // The direct parent's author: the lineage actor the attribution
                // stack credits before the original (KNOT-022-fix2).
                parent_author_avatar_url:
                  item.kind === 'version' ? item.parent_author_avatar_url : undefined,
                parent_author_display_name:
                  item.kind === 'version' ? item.parent_author_display_name : undefined,
                parent_author_id: item.kind === 'version' ? item.parent_author_id : undefined,
                title: item.title,
                version_count: item.version_count,
              }}
              variant={item.kind === 'version' ? 'version' : 'story'}
              versionNumber={item.kind === 'version' ? item.version_number : undefined}
              attributionLine={item.kind === 'version' ? item.attribution_line : undefined}
              // The feed closes each card with its own 4pt band, so the entry draws
              // no rule of its own (KNOT-023b, KNOT-020k-fix20).
              showBottomHairline={false}
              isMuted={isMuted}
              isVisible={item.id === activeItemId}
              onToggleMute={toggleMute}
              onPress={() =>
                item.kind === 'version' ? onOpenVersion(item.id) : onOpenStory(item.id)
              }
              onOpenAuthor={() => onOpenUserProfile(item.author_id)}
              onOpenMedia={onOpenMedia}
              onOpenOptions={() => setMenuItem(item)}
              // The one-tap hide asks first: it arms the confirmation rather than
              // removing the entry (KNOT-032-fix1).
              onHide={() => requestHide(item.id)}
            />
          </View>
        )}
        ListEmptyComponent={
          loading ? (
            <ActivityIndicator style={styles.spinner} />
          ) : error !== undefined ? (
            <Text style={styles.error}>{error}</Text>
          ) : (
            <EmptyState
              illustrationName="no-stories"
              headline="Stories will gather here."
              body="Choose the languages you read and we'll start there."
            />
          )
        }
        ListFooterComponent={
          loadingMore ? (
            <ActivityIndicator style={styles.footerSpinner} />
          ) : error !== undefined && items.length > 0 ? (
            <Text style={styles.error}>{error}</Text>
          ) : null
        }
      />

      <PostOptionsSheet
        item={menuItem}
        // The sheet's hide row asks exactly like the card's `✕` does: one shared
        // confirmation, one shared removal (KNOT-032-fix1).
        onHide={() => requestHide(menuItem?.id)}
        onReport={reportPost}
        onClose={closePostOptions}
      />

      <ReportModal
        visible={reportItem !== undefined}
        token={token}
        // A feed entry is either a story or somebody's version of one, so the
        // report names the thing that was actually on screen (KNOT-031).
        entityType={reportItem?.kind === 'version' ? 'version' : 'story'}
        entityId={reportItem?.id ?? ''}
        onClose={closeReport}
        onSubmitted={closeReport}
      />

      {/* The hide confirmation. The copy says "for now" because hiding is
          session-local: the entry returns on the next refresh, and the app must
          not imply a mute it cannot deliver (KNOT-032-fix1). */}
      <ConfirmSheet
        visible={pendingHideId !== undefined}
        title="Hide this story?"
        message="This story will be removed from your feed for now. It will be back the next time the feed refreshes."
        confirmLabel="Hide story"
        cancelLabel="Keep story"
        destructive
        onConfirm={confirmHide}
        onCancel={keepStory}
        testID="hide-confirm"
      />
    </View>
  );
}

/** The shuffle glyph on the prompt card's top row. */
const SHUFFLE_ICON_SIZE = 18;

/** The pencil on the card's call to action. */
const WRITE_ICON_SIZE = 18;

/**
 * The writing prompt at the top of the feed (KNOT-020k-fix7).
 *
 * It replaces the plain composer row: instead of asking an empty "what's on your
 * mind?", it asks one specific question, which is a much easier thing to answer.
 * The card carries the question's family (an overline and a 6pt mark), the
 * question itself at the second-largest type step, and two ways forward — the
 * primary "Write it" action, or a pillar chip that deals the next prompt of that
 * family. The shuffle deals the next prompt of any family.
 *
 * Both the question and the button open the composer: the button is the labelled
 * affordance, and the question is the larger target for a thumb that lands on the
 * text first. Nothing here talks to the network — the prompts are compiled into the
 * app (`feedPrompt.ts`) and choosing one only moves an index.
 *
 * It is local to this file rather than a shared component, because the feed is the
 * only place a prompt is drawn (the same choice the options sheet below makes).
 */
function PromptCard({
  prompt,
  onWrite,
  onShuffle,
  onSelectPillar,
}: {
  readonly prompt: FeedPrompt;
  readonly onWrite: () => void;
  readonly onShuffle: () => void;
  readonly onSelectPillar: (pillar: Pillar) => void;
}): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);

  return (
    <View style={styles.promptCard} testID="feed-prompt">
      <View style={styles.promptTop}>
        <View style={styles.promptFamily}>
          <View style={styles.promptDot} />
          {/* `typeScale.overline` carries the uppercasing, so the copy stays
              sentence case in the source. */}
          <Text style={[typeScale.overline, styles.promptFamilyText]}>
            {promptPillarLabel(prompt.pillar)}
          </Text>
        </View>
        <Pressable
          style={styles.promptShuffle}
          onPress={onShuffle}
          hitSlop={spacing.sm}
          accessibilityRole="button"
          accessibilityLabel="Show another prompt"
          testID="feed-prompt-shuffle"
        >
          <ArrowsClockwise size={SHUFFLE_ICON_SIZE} color={colors.text.tertiary} weight="bold" />
        </Pressable>
      </View>

      <Pressable
        onPress={onWrite}
        accessibilityRole="button"
        accessibilityLabel={`Write about this prompt: ${prompt.text}`}
        testID="feed-prompt-text"
      >
        <Text style={[typeScale.h2, styles.promptText]}>{prompt.text}</Text>
      </Pressable>

      <View style={styles.promptActions}>
        <Button
          label="Write it"
          onPress={onWrite}
          accessibilityLabel="Write a story from this prompt"
          leadingIcon={
            <PencilSimple size={WRITE_ICON_SIZE} color={colors.text.onBrand} weight="bold" />
          }
          testID="feed-prompt-write"
        />
        <View style={styles.promptChips}>
          {PROMPT_PILLARS.map((pillar) => {
            const chosen = pillar === prompt.pillar;

            return (
              <Chip
                key={pillar}
                label={promptPillarName(pillar)}
                // The two pillars are a choice, not a filter, so they read at the
                // button step and the chosen one is a solid surface rather than
                // merely tinted (KNOT-020k-fix8).
                textScale="button"
                selected={chosen}
                variant={chosen ? 'default' : 'outline'}
                onPress={() => onSelectPillar(pillar)}
                testID={`feed-prompt-chip-${pillar}`}
              />
            );
          })}
        </View>
      </View>
    </View>
  );
}

/**
 * The post options sheet: the actions a reader can take on one feed entry — hide
 * it from this feed, or report it to the moderators.
 *
 * Its hide row does not hide anything itself: it arms the feed's confirmation and
 * closes, so both entry points ask the same question (KNOT-032-fix1).
 *
 * It is local to this file rather than a shared component, because the feed is the
 * only place it is drawn (the same choice the profile's options sheet makes,
 * KNOT-020g-fix). A sheet rather than an inline popover keeps the rows at a
 * comfortable touch size on any card height and puts Cancel where the thumb
 * expects it (KNOT-031).
 */
function PostOptionsSheet({
  item,
  onHide,
  onReport,
  onClose,
}: {
  readonly item: FeedItem | undefined;
  readonly onHide: () => void;
  readonly onReport: () => void;
  readonly onClose: () => void;
}): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  return (
    <Modal visible={item !== undefined} transparent animationType="slide" onRequestClose={onClose}>
      <Pressable
        style={styles.sheetBackdrop}
        onPress={onClose}
        accessibilityRole="button"
        accessibilityLabel="Close the post options"
      >
        {/* The sheet swallows its own taps, so pressing a row never falls through
            to the backdrop and closes the menu before the action runs. */}
        <Pressable style={styles.sheet} onPress={() => undefined}>
          <View style={styles.sheetHandleArea}>
            <View style={styles.sheetHandle} />
          </View>

          <View style={styles.sheetBody}>
            <PostOptionRow icon={EyeSlash} label="Hide this post" onPress={onHide} />
            <PostOptionRow icon={Flag} label="Report post" danger onPress={onReport} />
          </View>

          <Pressable
            style={styles.sheetCancel}
            onPress={onClose}
            accessibilityRole="button"
            accessibilityLabel="Cancel"
          >
            <Text style={[typeScale.button, styles.sheetCancelText]}>Cancel</Text>
          </Pressable>
        </Pressable>
      </Pressable>
    </Modal>
  );
}

/**
 * One row of the post options sheet: a leading icon and a label, 44pt tall, with
 * `danger` reserved for the action that reports somebody's work.
 */
function PostOptionRow({
  icon,
  label,
  danger = false,
  onPress,
}: {
  readonly icon: Icon;
  readonly label: string;
  readonly danger?: boolean;
  readonly onPress: () => void;
}): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const IconComponent = icon;
  return (
    <Pressable
      style={styles.option}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={label}
    >
      <IconComponent
        color={danger ? colors.state.danger : colors.text.tertiary}
        size={20}
        weight="bold"
      />
      <Text style={[typeScale.body, styles.optionLabel, danger ? styles.optionLabelDanger : null]}>
        {label}
      </Text>
    </Pressable>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    // The avatar sits in the header's own gutter, one step in from the search
    // glyph's 44pt box so the pair reads as an icon and a face, not as two
    // buttons (KNOT-020j).
    avatar: {
      marginLeft: spacing.xs,
      marginRight: spacing.xs,
    },
    // One feed card: a full-width block on the canvas, closed by a 4pt band of the
    // separator neutral. The band — noticeably thicker than a hairline, and nothing
    // like the old 8pt gutter — is what parts two entries (KNOT-023b,
    // KNOT-032-fix1, KNOT-020k-fix20).
    card: {
      backgroundColor: colors.bg.canvas,
      borderBottomColor: colors.bg.separator,
      borderBottomWidth: 4,
    },
    // The page behind the cards is the canvas: with every entry closed by its own
    // rule there is no band left to paint, and a tinted page would show through
    // below the last card and behind the footer (KNOT-020k-fix19).
    container: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    // The band under the tab row, the same 4pt neutral that closes a card: one
    // surface, one thickness, full bleed (KNOT-020j-fix2, KNOT-020k-fix20).
    feedDivider: {
      backgroundColor: colors.bg.separator,
      height: 4,
    },
    // The prompt card: the one framed surface in the feed, and the only place a
    // radius is drawn on it (KNOT-020k-fix7). It sits on the raised surface with a
    // hairline rather than a shadow — the design system's rule — and its inset is
    // the feed's own 12pt step, so it reads as a card among edge-to-edge stories.
    promptCard: {
      backgroundColor: colors.bg.raised,
      borderColor: colors.border.hairline,
      borderRadius: radius.xxl,
      borderWidth: 1,
      gap: spacing.md,
      margin: spacing.md,
      padding: spacing.lg,
    },
    // The overline row: the family's dot and name on the left, the shuffle on the
    // right.
    promptTop: {
      alignItems: 'center',
      flexDirection: 'row',
      justifyContent: 'space-between',
    },
    promptFamily: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.xs,
    },
    // The 6pt mark that pairs with the overline's ink; the pillar's colour is the
    // one accent the card carries.
    promptDot: {
      backgroundColor: colors.brand.primary,
      borderRadius: radius.pill,
      height: 6,
      width: 6,
    },
    promptFamilyText: {
      color: colors.text.primary,
    },
    // 32pt of box with an 8pt hit slop, so the small glyph still has a 48pt target
    // without making the top row as tall as the button below it.
    promptShuffle: {
      alignItems: 'center',
      height: 32,
      justifyContent: 'center',
      width: 32,
    },
    // The question is the card's body: the screen's second-largest step, in the
    // primary ink, so it is the first thing read after the tab row.
    promptText: {
      color: colors.text.primary,
    },
    // The action row: the button is the anchor and never shrinks; the chips take
    // the rest and wrap rather than clipping on a narrow phone.
    promptActions: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.sm,
      justifyContent: 'space-between',
    },
    promptChips: {
      flexDirection: 'row',
      flexShrink: 1,
      flexWrap: 'wrap',
      gap: spacing.xs,
      justifyContent: 'flex-end',
    },
    error: {
      color: colors.state.danger,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.md,
      textAlign: 'center',
    },
    footerSpinner: {
      marginVertical: spacing.xl,
    },
    headerActions: {
      alignItems: 'center',
      flexDirection: 'row',
    },
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    listContent: {
      paddingBottom: spacing.xxxl,
    },
    // The Thread sits flush under the tab row: the list content starts there, so
    // the line reads as the refresh indicator rather than as part of a card.
    refreshingThread: {
      marginBottom: spacing.xs,
    },
    // One row of the post options sheet: a leading icon and the label, at the 44pt
    // minimum touch size. Rows are separated by spacing alone — no drawn dividers.
    option: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.md,
      minHeight: 44,
      paddingVertical: spacing.md,
    },
    optionLabel: {
      color: colors.text.primary,
      flex: 1,
      fontWeight: fontWeights.bold,
    },
    optionLabelDanger: {
      color: colors.state.danger,
    },
    // The options sheet: rounded top corners, the canvas fill, and the floating-bar
    // shadow (elevation 3, whose offset points up) — the profile sheet's treatment.
    sheet: {
      ...elevation[3],
      backgroundColor: colors.bg.sheet,
      borderTopLeftRadius: radius.xxl,
      borderTopRightRadius: radius.xxl,
      paddingBottom: spacing.xl,
    },
    sheetBackdrop: {
      backgroundColor: colors.scrim.overlay,
      flex: 1,
      justifyContent: 'flex-end',
    },
    sheetBody: {
      paddingHorizontal: spacing.lg,
    },
    // Cancel is a plain full-width row rather than a third option: it acts on the
    // sheet, not on the post.
    sheetCancel: {
      alignItems: 'center',
      borderTopColor: colors.border.hairline,
      borderTopWidth: 1,
      justifyContent: 'center',
      marginTop: spacing.sm,
      minHeight: 48,
      paddingTop: spacing.md,
    },
    sheetCancelText: {
      color: colors.text.secondary,
    },
    sheetHandle: {
      backgroundColor: colors.border.hairline,
      borderRadius: radius.pill,
      height: 4,
      width: 32,
    },
    sheetHandleArea: {
      alignItems: 'center',
      paddingBottom: spacing.sm,
      paddingTop: spacing.sm,
    },
    spinner: {
      marginTop: spacing.xl,
    },
    // One tab, one quarter of the row: the label is centred and single-lined, so it
    // cannot clip. The 2px underline sits below the label, over the row's hairline.
    // The four-column tab. The row draws no rule of its own: the feed's 4pt band
    // below it does that job (KNOT-020j-fix2, KNOT-020k-fix20).
    tab: {
      alignItems: 'center',
      flex: 1,
      paddingBottom: spacing.sm,
      paddingTop: spacing.md,
      position: 'relative',
    },
    // The selected tab's marker: a 2pt indigo rule along the tab's own width,
    // drawn only for the active tab.
    tabUnderline: {
      backgroundColor: colors.brand.primary,
      bottom: 0,
      height: 2,
      left: 0,
      position: 'absolute',
      right: 0,
    },
    tabLabel: {
      color: colors.text.tertiary,
      textAlign: 'center',
    },
    tabLabelActive: {
      color: colors.text.primary,
    },
    tabs: {
      flexDirection: 'row',
      paddingHorizontal: spacing.lg,
    },
    topBar: {
      backgroundColor: colors.bg.canvas,
      zIndex: 1,
    },
  });

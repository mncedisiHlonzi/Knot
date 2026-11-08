import type { FeedItem } from '../../api/stories';
import type { FeedEntryStory } from '../../components/FeedEntry';

/**
 * Folds one feed entry onto the flat story shape `FeedEntry` draws.
 *
 * The wire sends a discriminated union: a story entry IS a `Story`, while an
 * adaptation repeats the story-shaped fields a card needs. Turning either into
 * the entry's own shape used to happen inside `renderItem`, which runs on every
 * list render — so each mounted card received a brand new `story` object and
 * re-rendered, players and all, every time the screen's state changed while
 * scrolling (KNOT-049).
 *
 * Building it once per item — see `entryStoryMap` — gives the memoised entry the
 * same object reference for as long as the item itself is unchanged, which is
 * what lets it skip those renders entirely.
 */
export function feedEntryStory(item: FeedItem): FeedEntryStory {
  const isVersion = item.kind === 'version';

  return {
    author_avatar_url: item.author_avatar_url,
    author_display_name: item.author_display_name,
    author_rooted: item.author_rooted ?? null,
    comment_count: item.comment_count,
    created_at: item.created_at,
    first_media_type: item.first_media_type ?? null,
    first_media_url: item.first_media_url ?? null,
    // The still a video opens on. Without it the card has nothing to draw over
    // the player until the first frame decodes, which on a slow connection is
    // the entire download (KNOT-048-fix).
    first_media_poster_url: item.first_media_poster_url ?? null,
    first_media_width: item.first_media_width ?? null,
    first_media_height: item.first_media_height ?? null,
    // The duration the payload already knows, so a video's badge is right before
    // the player has read the file. Only a story entry carries its own media
    // array; a version entry has no duration on the wire, so it stays null and
    // the badge waits for `onLoad` (KNOT-048-fix).
    duration_ms: item.kind === 'story' ? (item.media?.[0]?.duration_ms ?? null) : null,
    id: item.id,
    // The story a version entry adapts, so the viewer's "View Story" has a story
    // to open; a story entry's own id is the story (KNOT-020k-fix2).
    storyId: isVersion ? item.story_id : item.id,
    language: item.language,
    language_count: item.language_count,
    media_count: item.media_count,
    pillar: item.pillar,
    place: item.approximate_location,
    preview: item.body,
    preview_truncated: item.preview_truncated,
    reactions: item.reactions,
    recent_interactions: item.recent_interactions,
    // The original author's identity, only on an adaptation; the card heads a
    // version with it. A story entry has no such fields (KNOT-023a).
    root_author_avatar_url: isVersion ? item.root_author_avatar_url : undefined,
    root_author_display_name: isVersion ? item.root_author_display_name : undefined,
    root_author_id: isVersion ? item.root_author_id : undefined,
    // The direct parent's author: the lineage actor the attribution stack
    // credits before the original (KNOT-022-fix2).
    parent_author_avatar_url: isVersion ? item.parent_author_avatar_url : undefined,
    parent_author_display_name: isVersion ? item.parent_author_display_name : undefined,
    parent_author_id: isVersion ? item.parent_author_id : undefined,
    title: item.title,
    version_count: item.version_count,
  };
}

/**
 * One prepared story object per entry, keyed by id.
 *
 * The screen holds the result in a `useMemo` keyed on its `items`, so the
 * objects survive every re-render that does not change the list itself — which
 * is all of them while a person scrolls (KNOT-049).
 */
export function entryStoryMap(items: readonly FeedItem[]): ReadonlyMap<string, FeedEntryStory> {
  const stories = new Map<string, FeedEntryStory>();

  for (const item of items) {
    stories.set(item.id, feedEntryStory(item));
  }

  return stories;
}

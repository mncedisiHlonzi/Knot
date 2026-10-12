import React from 'react';
import { Image, Pressable, StyleSheet, Text, View } from 'react-native';
import { Images } from 'phosphor-react-native';

import { API_BASE_URL } from '../config/api';
import { colors, layout, radius, spacing, tabularNums, typeScale } from '../theme';
import AuthorLine from './AuthorLine';
import { feedMetaLine } from './feedEntryMeta';
import FeedVideo, { VIDEO_MUTE_BUTTON_SIZE } from './FeedVideo';
import PillarTag from './PillarTag';

/** The story shape a feed entry renders. */
type FeedEntryStory = {
  readonly id: string;
  readonly title: string;
  readonly pillar: 'wonder' | 'heritage';
  readonly preview: string;
  readonly language: string;
  readonly place?: string | null;
  readonly author_display_name?: string | null;
  readonly author_avatar_url?: string | null;
  readonly author_rooted?: { readonly place: string; readonly duration_bucket: string } | null;
  readonly created_at: string;
  readonly version_count?: number;
  readonly language_count?: number;
  readonly comment_count?: number;
  readonly media_count?: number;
  /** The first media item's kind, or null when the story has no media. */
  readonly first_media_type?: 'image' | 'video' | null;
  /** The first media item's relative path on the API, or null/empty for none. */
  readonly first_media_url?: string | null;
  readonly reactions?: {
    readonly rings_true: number;
    readonly know_it_differently: number;
    readonly adds_something_new: number;
    readonly needs_a_source: number;
  };
};

type FeedEntryProps = {
  /** The story to render. */
  readonly story: FeedEntryStory;
  /** Called when the entry is tapped. */
  readonly onPress: () => void;
  /** Called when the author's identity is tapped. */
  readonly onOpenAuthor?: () => void;
  /**
   * True when this entry is the one the feed judges to be on screen. It is the
   * single input that plays or pauses the entry's video, so only one video in the
   * feed is ever playing (KNOT-020c-fix3). Defaults to `false`.
   */
  readonly isVisible?: boolean;
  /** The feed-wide mute state, shared by every video. Defaults to muted. */
  readonly isMuted?: boolean;
  /** Called when the entry's mute control is tapped. */
  readonly onToggleMute?: () => void;
  /** Test identifier. */
  readonly testID?: string;
};

/**
 * The media preview's aspect ratio (16:9), matching Story Detail's media strip.
 */
const MEDIA_ASPECT_RATIO = 16 / 9;

/** The preview's line limit. */
const PREVIEW_LINES = 3;

/** The media-count badge's glyph size. */
const COUNT_ICON_SIZE = 14;

/** A no-op for the mute control when a caller renders an entry without wiring one. */
const NOOP = (): void => undefined;

/**
 * Resolves a media path returned by the API against the base URL.
 *
 * The API sends a relative path (`/stories/{id}/media/{mid}/content`), so it is
 * joined to the API base. A missing or blank path resolves to the empty string,
 * which the caller reads as "no media".
 */
function resolveMediaUrl(path: string | null | undefined): string {
  const trimmed = typeof path === 'string' ? path.trim() : '';
  return trimmed === '' ? '' : `${API_BASE_URL}${trimmed}`;
}

/**
 * One story in the feed, edge-to-edge with hairlines between entries.
 *
 * It is a full-bleed row on the canvas — no card, no border, no shadow — so the
 * feed reads as one continuous column. Its order is the reading order: the author
 * block, the pillar tag, the title, the opening lines, the media, then the meta
 * line. The whole entry is one tap target; the author identity inside it is a
 * nested target that opens the profile (KNOT-ADR-043).
 *
 * The author block is the design reference's three lines: avatar and name with the
 * relative time, then the quiet Rooted line (no dot, no pill), then the plain
 * pillar tag (no fill) — which is why the tag is asked for its `plain` variant
 * (KNOT-020c-fix3).
 *
 * The text column keeps the feed's horizontal inset, but the **media is
 * full-bleed**: no horizontal padding and no corner rounding. An image draws
 * directly; a video is handed to `FeedVideo`, which plays it in place when this
 * entry is the visible one. The count badge stacks above the video's mute control
 * so the two never collide.
 *
 * This component stays **hookless** on purpose — the tests call it as a plain
 * function — so all playback logic lives in `FeedVideo`.
 */
export default function FeedEntry({
  story,
  onPress,
  onOpenAuthor,
  isVisible = false,
  isMuted = true,
  onToggleMute = NOOP,
  testID,
}: FeedEntryProps): React.ReactElement {
  const mediaUrl = resolveMediaUrl(story.first_media_url);
  const isVideo = story.first_media_type === 'video';
  const mediaCount = story.media_count ?? 0;

  const metaLine = feedMetaLine({
    commentCount: story.comment_count,
    languageCount: story.language_count,
    reactions: story.reactions,
    versionCount: story.version_count,
  });

  return (
    <Pressable
      style={styles.entry}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`Open the story: ${story.title}`}
      testID={testID}
    >
      <View style={styles.padded}>
        <AuthorLine
          displayName={story.author_display_name}
          avatarUrl={story.author_avatar_url}
          createdAt={story.created_at}
          rooted={story.author_rooted ?? null}
          size="medium"
          showRootedOnNewLine
          onPress={onOpenAuthor}
        />

        <View style={styles.tagRow}>
          <PillarTag pillar={story.pillar} place={story.place} variant="plain" />
        </View>

        <Text style={[typeScale.title, styles.title]}>{story.title}</Text>
        <Text style={[typeScale.body, styles.preview]} numberOfLines={PREVIEW_LINES}>
          {story.preview}
        </Text>
      </View>

      {mediaUrl !== '' ? (
        <View style={styles.mediaFrame}>
          {isVideo ? (
            <FeedVideo
              uri={mediaUrl}
              isVisible={isVisible}
              isMuted={isMuted}
              onToggleMute={onToggleMute}
            />
          ) : (
            <Image
              source={{ uri: mediaUrl }}
              style={styles.media}
              resizeMode="cover"
              accessibilityIgnoresInvertColors
            />
          )}

          {mediaCount > 1 ? (
            <View
              style={[styles.countBadge, isVideo ? styles.countBadgeOverVideo : null]}
              accessibilityLabel={`${mediaCount} media items`}
            >
              <Images color={colors.text.onBrand} size={COUNT_ICON_SIZE} weight="bold" />
              <Text style={[typeScale.caption, tabularNums, styles.countBadgeText]}>
                {mediaCount}
              </Text>
            </View>
          ) : null}
        </View>
      ) : null}

      <View style={styles.padded}>
        {metaLine !== '' ? (
          <Text style={[typeScale.caption, tabularNums, styles.metaText]}>{metaLine}</Text>
        ) : null}

        <View style={styles.hairline} />
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  countBadge: {
    alignItems: 'center',
    backgroundColor: colors.scrim.overlay,
    borderRadius: radius.sm,
    bottom: spacing.sm,
    flexDirection: 'row',
    gap: spacing.xs,
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    position: 'absolute',
    right: spacing.sm,
  },
  // A video owns the bottom-right corner with its mute control, so the count
  // badge sits directly above it instead of underneath it.
  countBadgeOverVideo: {
    bottom: spacing.sm + VIDEO_MUTE_BUTTON_SIZE + spacing.xs,
  },
  countBadgeText: {
    color: colors.text.onBrand,
  },
  entry: {
    paddingTop: spacing.lg,
  },
  hairline: {
    backgroundColor: colors.border.hairline,
    height: 1,
    marginTop: spacing.lg,
  },
  media: {
    height: '100%',
    width: '100%',
  },
  // Full-bleed: no horizontal inset and no rounding, so the media runs edge to
  // edge (KNOT-020c-fix2).
  mediaFrame: {
    aspectRatio: MEDIA_ASPECT_RATIO,
    backgroundColor: colors.bg.sunken,
    marginTop: spacing.md,
    overflow: 'hidden',
    width: '100%',
  },
  metaText: {
    color: colors.text.tertiary,
    marginTop: spacing.md,
  },
  // The text column keeps the feed inset; only the media leaves it.
  padded: {
    paddingHorizontal: layout.screenPaddingFeed,
  },
  preview: {
    color: colors.text.secondary,
    marginTop: spacing.xs,
  },
  tagRow: {
    marginTop: spacing.md,
  },
  title: {
    color: colors.text.primary,
    marginTop: spacing.sm,
  },
});

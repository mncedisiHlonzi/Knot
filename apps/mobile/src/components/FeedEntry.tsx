import React from 'react';
import { Image, Pressable, StyleSheet, Text, View } from 'react-native';
import { Images, PlayCircle } from 'phosphor-react-native';
import Video from 'react-native-video';

import { API_BASE_URL } from '../config/api';
import { colors, layout, radius, spacing, tabularNums, typeScale } from '../theme';
import AuthorLine from './AuthorLine';
import { feedMetaLine } from './feedEntryMeta';
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
  /** Test identifier. */
  readonly testID?: string;
};

/**
 * The media preview's aspect ratio (16:9), matching Story Detail's media strip.
 *
 * The design reference's thumbnail is a little wider than this, but 16:9 is the
 * ratio the feed already used and it keeps the entry to a comfortable height
 * (KNOT-020c-fix2).
 */
const MEDIA_ASPECT_RATIO = 16 / 9;

/** The preview's line limit. */
const PREVIEW_LINES = 3;

/** The play glyph drawn over a video preview. */
const PLAY_ICON_SIZE = 48;

/** The circular scrim behind the play glyph. */
const PLAY_SCRIM_SIZE = 64;

/** The media-count badge's glyph size. */
const COUNT_ICON_SIZE = 14;

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
 * feed reads as one continuous column. Its order is the reading order: who told
 * it, which pillar and place, the title, the opening lines, the media, then the
 * meta line. The whole entry is one tap target; the author identity inside it is
 * a nested target that opens the profile (KNOT-ADR-043).
 *
 * The text column keeps the feed's horizontal inset, but the **media is
 * full-bleed**: no horizontal padding, no corner rounding, so a photo or a video
 * runs edge to edge the way a social feed's media does (KNOT-020c-fix2).
 *
 * A video preview draws the first frame with `react-native-video`, paused and
 * muted, with a play glyph on a scrim over it. No poster frame is stored, so a
 * grey fill sits behind the video: it shows through while the frame loads or if
 * the video fails, which is the fallback (KNOT-020c-fix2).
 *
 * The meta line is plain text — "4 versions · 3 languages · 128 reactions ·
 * 14 comments" — with no glyphs, each segment omitted when its count is zero.
 */
export default function FeedEntry({
  story,
  onPress,
  onOpenAuthor,
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
          <PillarTag pillar={story.pillar} place={story.place} />
        </View>

        <Text style={[typeScale.title, styles.title]}>{story.title}</Text>
        <Text style={[typeScale.body, styles.preview]} numberOfLines={PREVIEW_LINES}>
          {story.preview}
        </Text>
      </View>

      {mediaUrl !== '' ? (
        <View style={styles.mediaFrame}>
          {isVideo ? (
            <>
              {/* Shown while the first frame loads, and kept if the video cannot
                  play at all. */}
              <View style={styles.videoFallback} />

              <Video
                source={{ uri: mediaUrl }}
                style={styles.media}
                paused
                muted
                resizeMode="cover"
              />

              <View style={styles.playScrim} pointerEvents="none">
                <PlayCircle color={colors.text.onBrand} size={PLAY_ICON_SIZE} />
              </View>

              <View style={styles.mediaTag}>
                <Text style={[typeScale.caption, styles.mediaTagText]}>Video</Text>
              </View>
            </>
          ) : (
            <Image
              source={{ uri: mediaUrl }}
              style={styles.media}
              resizeMode="cover"
              accessibilityIgnoresInvertColors
            />
          )}

          {mediaCount > 1 ? (
            <View style={styles.countBadge} accessibilityLabel={`${mediaCount} media items`}>
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
  mediaTag: {
    backgroundColor: colors.scrim.overlay,
    borderRadius: radius.sm,
    left: spacing.sm,
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    position: 'absolute',
    top: spacing.sm,
  },
  mediaTagText: {
    color: colors.text.onBrand,
  },
  metaText: {
    color: colors.text.tertiary,
    marginTop: spacing.md,
  },
  // The text column keeps the feed inset; only the media leaves it.
  padded: {
    paddingHorizontal: layout.screenPaddingFeed,
  },
  playScrim: {
    alignItems: 'center',
    backgroundColor: colors.scrim.overlay,
    borderRadius: radius.pill,
    height: PLAY_SCRIM_SIZE,
    justifyContent: 'center',
    left: '50%',
    marginLeft: -PLAY_SCRIM_SIZE / 2,
    marginTop: -PLAY_SCRIM_SIZE / 2,
    position: 'absolute',
    top: '50%',
    width: PLAY_SCRIM_SIZE,
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
  videoFallback: {
    ...StyleSheet.absoluteFillObject,
  },
});

import React, { Fragment } from 'react';
import { Image, Pressable, StyleSheet, Text, View } from 'react-native';
import { Images, PlayCircle } from 'phosphor-react-native';

import RingsTrueGlyph from '../assets/brand/reaction-rings-true.svg';
import { API_BASE_URL } from '../config/api';
import { colors, layout, radius, spacing, tabularNums, typeScale } from '../theme';
import AuthorLine from './AuthorLine';
import { feedMetaSegments } from './feedEntryMeta';
import PillarTag from './PillarTag';
import SvgImage from './SvgImage';

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

/** The media preview's aspect ratio (16:9). */
const MEDIA_ASPECT_RATIO = 16 / 9;

/** The preview's line limit. */
const PREVIEW_LINES = 3;

/** The play glyph drawn in the video placeholder. */
const PLAY_ICON_SIZE = 48;

/** The media-count badge's glyph size. */
const COUNT_ICON_SIZE = 14;

/** The reaction glyph's size inside the meta row. */
const META_GLYPH_SIZE = 14;

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
 * it, which pillar and place, the title, the opening lines, an optional media
 * preview, then the meta row. The whole entry is one tap target; the author
 * identity inside it is a nested target that opens the profile (KNOT-ADR-043).
 *
 * The media preview is an image, or — for a video — a placeholder with a play
 * glyph, because the backend stores no poster frame yet (KNOT-020c-fix). When the
 * story has more than one media item, a count badge sits in the preview's corner.
 * The meta row carries versions, languages, the reactions total, and comments,
 * each omitted when zero.
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

  const meta = feedMetaSegments({
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
      <AuthorLine
        displayName={story.author_display_name}
        avatarUrl={story.author_avatar_url}
        createdAt={story.created_at}
        rooted={story.author_rooted ?? null}
        size="small"
        onPress={onOpenAuthor}
      />

      <View style={styles.tagRow}>
        <PillarTag pillar={story.pillar} place={story.place} />
      </View>

      <Text style={[typeScale.title, styles.title]}>{story.title}</Text>
      <Text style={[typeScale.body, styles.preview]} numberOfLines={PREVIEW_LINES}>
        {story.preview}
      </Text>

      {mediaUrl !== '' ? (
        <View style={styles.mediaFrame}>
          {isVideo ? (
            <>
              <View style={styles.videoPlaceholder}>
                <PlayCircle color={colors.brand.primary} size={PLAY_ICON_SIZE} />
              </View>
              <View style={styles.mediaTag}>
                <Text style={[typeScale.caption, styles.mediaTagText]}>Video</Text>
              </View>
            </>
          ) : (
            <Image
              source={{ uri: mediaUrl }}
              style={styles.mediaImage}
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

      {meta.length > 0 ? (
        <View style={styles.metaRow}>
          {meta.map((segment, index) => (
            <Fragment key={segment.key}>
              {index > 0 ? <Text style={[typeScale.caption, styles.metaText]}>·</Text> : null}
              {segment.withGlyph ? (
                <SvgImage
                  source={RingsTrueGlyph}
                  size={META_GLYPH_SIZE}
                  color={colors.text.tertiary}
                  decorative
                />
              ) : null}
              <Text
                style={[typeScale.caption, tabularNums, styles.metaText]}
                accessibilityLabel={segment.accessibilityLabel}
              >
                {segment.label}
              </Text>
            </Fragment>
          ))}
        </View>
      ) : null}

      <View style={styles.hairline} />
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
    paddingHorizontal: layout.screenPaddingFeed,
    paddingTop: spacing.lg,
  },
  hairline: {
    backgroundColor: colors.border.hairline,
    height: 1,
    marginTop: spacing.lg,
  },
  mediaFrame: {
    aspectRatio: MEDIA_ASPECT_RATIO,
    backgroundColor: colors.bg.sunken,
    borderRadius: radius.lg,
    marginTop: spacing.md,
    overflow: 'hidden',
    width: '100%',
  },
  mediaImage: {
    height: '100%',
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
  metaRow: {
    alignItems: 'center',
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
    marginTop: spacing.md,
  },
  metaText: {
    color: colors.text.tertiary,
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
  videoPlaceholder: {
    ...StyleSheet.absoluteFillObject,
    alignItems: 'center',
    justifyContent: 'center',
  },
});

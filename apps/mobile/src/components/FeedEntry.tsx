import React from 'react';
import { Image, Pressable, StyleSheet, Text, View } from 'react-native';

import { colors, layout, radius, spacing, typeScale } from '../theme';
import AuthorLine from './AuthorLine';
import { feedMeta } from './feedEntryMeta';
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
  readonly media_preview_url?: string | null;
  readonly reactions?: {
    readonly rings_true: number;
    readonly know_it_differently: number;
    readonly adds_something_new: number;
    readonly needs_a_source: number;
  };
  readonly comment_count?: number;
};

type FeedEntryProps = {
  /** The story to render. */
  readonly story: FeedEntryStory;
  /** Called when the entry is tapped. */
  readonly onPress: () => void;
  /** Called when the author's identity is tapped. */
  readonly onOpenAuthor?: () => void;
  /** Called when the version count is tapped, to open the language tree. */
  readonly onOpenTree?: () => void;
  /** Test identifier. */
  readonly testID?: string;
};

/** The media preview's aspect ratio (16:9). */
const MEDIA_ASPECT_RATIO = 16 / 9;

/** The preview's line limit. */
const PREVIEW_LINES = 3;

/**
 * One story in the feed, edge-to-edge with hairlines between entries.
 *
 * It is a full-bleed row on the canvas — no card, no border, no shadow — so the
 * feed reads as one continuous column. Its order is the reading order: who told
 * it, which pillar and place, the title, the opening lines, an optional media
 * preview, then the meta row. The whole entry is one tap target; the author
 * identity inside it is a nested target that opens the profile (KNOT-ADR-043),
 * and the version count opens the language tree.
 */
export default function FeedEntry({
  story,
  onPress,
  onOpenAuthor,
  onOpenTree,
  testID,
}: FeedEntryProps): React.ReactElement {
  const media = typeof story.media_preview_url === 'string' ? story.media_preview_url.trim() : '';
  const meta = feedMeta({
    versionCount: story.version_count,
    reactions: story.reactions,
    commentCount: story.comment_count,
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

      {media !== '' ? (
        <Image
          source={{ uri: media }}
          style={styles.media}
          resizeMode="cover"
          accessibilityIgnoresInvertColors
        />
      ) : null}

      {meta.versions !== '' || meta.suffix !== '' ? (
        <View style={styles.metaRow}>
          {meta.versions !== '' ? (
            <Pressable
              onPress={onOpenTree}
              disabled={onOpenTree === undefined}
              hitSlop={spacing.sm}
              accessibilityRole={onOpenTree === undefined ? 'text' : 'button'}
              accessibilityLabel={onOpenTree === undefined ? undefined : 'Open the language tree'}
            >
              <Text style={[typeScale.caption, styles.metaText]}>{meta.versions}</Text>
            </Pressable>
          ) : null}
          {meta.suffix !== '' ? (
            <Text style={[typeScale.caption, styles.metaText]}>
              {meta.versions !== '' ? ` · ${meta.suffix}` : meta.suffix}
            </Text>
          ) : null}
        </View>
      ) : null}

      <View style={styles.hairline} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  entry: {
    paddingHorizontal: layout.screenPaddingFeed,
    paddingTop: spacing.lg,
  },
  hairline: {
    backgroundColor: colors.border.hairline,
    height: 1,
    marginTop: spacing.lg,
  },
  media: {
    aspectRatio: MEDIA_ASPECT_RATIO,
    borderRadius: radius.lg,
    marginTop: spacing.md,
    width: '100%',
  },
  metaRow: {
    alignItems: 'center',
    flexDirection: 'row',
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
});

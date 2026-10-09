import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Image,
  Pressable,
  ScrollView,
  StyleProp,
  StyleSheet,
  Text,
  View,
  ViewStyle,
} from 'react-native';
import Video from 'react-native-video';

import { describeError } from '../../api/client';
import { Story, StoryMedia, storiesApi } from '../../api/stories';
import { versionsApi } from '../../api/versions';
import AuthorLine from '../../components/AuthorLine';
import CameraCaptureBadge from '../../components/CameraCaptureBadge';
import MediaGalleryModal from '../../components/MediaGalleryModal';
import { API_BASE_URL } from '../../config/api';
import { colors, fontSizes, fontWeights, lineHeights, radius, spacing } from '../../theme';

/** Resolves a relative media path returned by the API against the base URL. */
function resolveMediaUrl(url: string): string {
  return `${API_BASE_URL}${url}`;
}

/** A still image or a paused video with a play overlay and the capture badge. */
function MediaTile({
  media,
  onOpen,
  style,
}: {
  readonly media: StoryMedia;
  readonly onOpen: () => void;
  readonly style?: StyleProp<ViewStyle>;
}): React.ReactElement {
  const [paused, setPaused] = useState(true);
  const uri = resolveMediaUrl(media.url);

  return (
    <View style={[styles.tile, style]}>
      {/* A full-tile tap target, under the play button so the play button still
          receives its own taps. */}
      <Pressable
        style={styles.tileOpenTarget}
        onPress={onOpen}
        accessibilityRole="button"
        accessibilityLabel="Open media full screen"
      />

      {media.media_type === 'video' ? (
        <Video
          source={{ uri }}
          style={styles.tileMedia}
          paused={paused}
          resizeMode="cover"
          repeat
          muted
        />
      ) : (
        <Image style={styles.tileMedia} source={{ uri }} resizeMode="cover" />
      )}

      {media.media_type === 'video' && paused ? (
        <Pressable
          style={styles.playOverlay}
          onPress={() => setPaused(false)}
          accessibilityRole="button"
          accessibilityLabel="Play video"
        >
          <Text style={styles.playOverlayIcon}>▶</Text>
        </Pressable>
      ) : null}

      {media.source === 'camera' ? <CameraCaptureBadge style={styles.tileBadge} /> : null}
    </View>
  );
}

type StoryDetailScreenProps = {
  /** The id of the story to read. */
  readonly id: string;
  /** Called when the person returns to the feed. */
  readonly onBack: () => void;
  /**
   * Called to adapt this story. The argument is the story's root version id,
   * which is where an adaptation starts.
   */
  readonly onAdapt: (rootVersionId: string) => void;
  /** Called when the person wants to see every version of the story. */
  readonly onViewTree: () => void;
  /**
   * Called to open the conversation on a version. The argument is the version
   * id, which is the story's root version from this screen.
   */
  readonly onConversation: (versionId: string) => void;
  /** Called when the story's author is tapped, to open their profile. */
  readonly onOpenUserProfile: (userId: string) => void;
};

/**
 * One story in full.
 *
 * A story that does not exist is reported as a normal error message rather than a
 * crash: the server answers 404 with a readable message, and `describeError`
 * surfaces it verbatim.
 */
export default function StoryDetailScreen({
  id,
  onBack,
  onAdapt,
  onViewTree,
  onConversation,
  onOpenUserProfile,
}: StoryDetailScreenProps): React.ReactElement {
  const [story, setStory] = useState<Story | undefined>(undefined);
  const [versionCount, setVersionCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);
  // The index of the item shown full screen, or undefined when the viewer is
  // closed.
  const [viewerIndex, setViewerIndex] = useState<number | undefined>(undefined);

  const media = story?.media ?? [];

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      // The story carries its root version's content; the tree carries every
      // version of it, so the count comes from the tree.
      const [storyResult, treeResult] = await Promise.all([
        storiesApi.getStory(id),
        versionsApi.getTree(id),
      ]);
      setStory(storyResult.story);
      setVersionCount(treeResult.versions.length);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <ScrollView contentContainerStyle={styles.content}>
      <Pressable style={styles.link} onPress={onBack}>
        <Text style={styles.linkText}>← Back</Text>
      </Pressable>

      {loading ? <ActivityIndicator style={styles.spinner} /> : null}
      {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

      {story !== undefined ? (
        <>
          <Text style={styles.title}>{story.title}</Text>
          <View style={styles.authorRow}>
            <AuthorLine
              displayName={story.author_display_name}
              avatarUrl={story.author_avatar_url}
              createdAt={story.created_at}
              rooted={story.author_rooted}
              size="medium"
              onPress={() => onOpenUserProfile(story.author_id)}
            />
          </View>
          <Text style={styles.meta}>
            {story.pillar} · {story.language}
            {story.sensitive ? ' · sensitive' : ''}
          </Text>
          {story.approximate_location !== '' ? (
            <Text style={styles.meta}>{story.approximate_location}</Text>
          ) : null}

          <Text style={styles.body}>{story.body}</Text>

          {media.length === 1 ? (
            <MediaTile
              media={media[0]}
              onOpen={() => setViewerIndex(0)}
              style={styles.singleMedia}
            />
          ) : null}

          {media.length > 1 ? (
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              style={styles.carousel}
              contentContainerStyle={styles.carouselContent}
            >
              {media.map((item, index) => (
                <MediaTile
                  key={item.id}
                  media={item}
                  onOpen={() => setViewerIndex(index)}
                  style={styles.carouselItem}
                />
              ))}
            </ScrollView>
          ) : null}

          {media.length === 0 && story.media_urls.length > 0 ? (
            <Text style={styles.meta}>Attached: {story.media_urls.join(', ')}</Text>
          ) : null}

          <Pressable style={styles.primaryButton} onPress={() => onAdapt(story.root_version_id)}>
            <Text style={styles.primaryButtonText}>Adapt for my people</Text>
          </Pressable>
          <Pressable style={styles.secondaryButton} onPress={onViewTree}>
            <Text style={styles.secondaryButtonText}>
              View language tree ({versionCount} {versionCount === 1 ? 'version' : 'versions'})
            </Text>
          </Pressable>
          <Pressable
            style={styles.secondaryButton}
            onPress={() => onConversation(story.root_version_id)}
          >
            <Text style={styles.secondaryButtonText}>See conversation</Text>
          </Pressable>

          <MediaGalleryModal
            visible={viewerIndex !== undefined}
            media={media}
            initialIndex={viewerIndex ?? 0}
            onClose={() => setViewerIndex(undefined)}
          />
        </>
      ) : null}

      {!loading && story === undefined && error !== undefined ? (
        <Pressable style={styles.secondaryButton} onPress={load}>
          <Text style={styles.secondaryButtonText}>Try again</Text>
        </Pressable>
      ) : null}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  authorRow: {
    marginTop: spacing.sm,
  },
  body: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    lineHeight: lineHeights.md,
    marginTop: 20,
  },
  carousel: {
    marginTop: spacing.lg,
  },
  carouselContent: {
    paddingRight: spacing.md,
  },
  carouselItem: {
    marginRight: spacing.md,
  },
  content: {
    backgroundColor: colors.bg.primary,
    padding: spacing.xl,
    paddingBottom: spacing['3xl'],
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.base,
    marginTop: spacing.lg,
  },
  link: {
    marginBottom: spacing.md,
  },
  linkText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
  },
  meta: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: 6,
  },
  playOverlay: {
    alignItems: 'center',
    backgroundColor: colors.overlay,
    borderRadius: radius.pill,
    height: 44,
    justifyContent: 'center',
    left: '50%',
    marginLeft: -22,
    marginTop: -22,
    position: 'absolute',
    top: '50%',
    width: 44,
  },
  playOverlayIcon: {
    color: colors.text.primary,
    fontSize: fontSizes.lg,
    marginLeft: 3,
  },
  secondaryButton: {
    alignItems: 'center',
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.md,
    paddingVertical: spacing.md,
  },
  secondaryButtonText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  singleMedia: {
    marginTop: spacing.lg,
    width: '100%',
  },
  tile: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.subtle,
    borderRadius: radius.md,
    borderWidth: 1,
    height: 200,
    overflow: 'hidden',
    width: 200,
  },
  tileBadge: {
    left: spacing.xs,
    position: 'absolute',
    top: spacing.xs,
  },
  tileMedia: {
    height: '100%',
    width: '100%',
  },
  tileOpenTarget: {
    ...StyleSheet.absoluteFillObject,
    zIndex: 1,
  },
  primaryButton: {
    alignItems: 'center',
    backgroundColor: colors.brand.purple,
    borderRadius: radius.md,
    marginTop: spacing.xl,
    paddingVertical: 14,
  },
  primaryButtonText: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    fontWeight: fontWeights.semiBold,
  },
  spinner: {
    marginTop: spacing.xl,
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
    marginTop: spacing.sm,
  },
});

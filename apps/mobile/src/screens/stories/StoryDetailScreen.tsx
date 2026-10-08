import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { describeError } from '../../api/client';
import { Story, storiesApi } from '../../api/stories';
import { versionsApi } from '../../api/versions';
import RootedBadge from '../../components/RootedBadge';
import { colors, fontSizes, fontWeights, lineHeights, radius, spacing } from '../../theme';

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
}: StoryDetailScreenProps): React.ReactElement {
  const [story, setStory] = useState<Story | undefined>(undefined);
  const [versionCount, setVersionCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);

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
          <Text style={styles.meta}>
            {story.pillar} · {story.language} · {story.created_at}
            {story.sensitive ? ' · sensitive' : ''}
          </Text>
          {story.approximate_location !== '' ? (
            <Text style={styles.meta}>{story.approximate_location}</Text>
          ) : null}

          {story.author_rooted ? (
            <View style={styles.rootedRow}>
              <RootedBadge
                place={story.author_rooted.place}
                durationBucket={story.author_rooted.duration_bucket}
              />
            </View>
          ) : null}

          <Text style={styles.body}>{story.body}</Text>

          {story.media_urls.length > 0 ? (
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
  body: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    lineHeight: lineHeights.md,
    marginTop: 20,
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
  rootedRow: {
    marginTop: spacing.sm,
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

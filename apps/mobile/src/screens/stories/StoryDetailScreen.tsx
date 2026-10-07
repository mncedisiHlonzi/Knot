import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text } from 'react-native';

import { describeError } from '../../api/client';
import { Story, storiesApi } from '../../api/stories';
import { versionsApi } from '../../api/versions';

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
        <Text style={styles.linkText}>Back to stories</Text>
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
    color: '#24292f',
    fontSize: 16,
    lineHeight: 24,
    marginTop: 20,
  },
  content: {
    padding: 24,
    paddingBottom: 48,
  },
  error: {
    color: '#b3261e',
    fontSize: 14,
    marginTop: 16,
  },
  link: {
    marginBottom: 12,
  },
  linkText: {
    color: '#1f6feb',
    fontSize: 15,
  },
  meta: {
    color: '#57606a',
    fontSize: 13,
    marginTop: 6,
  },
  secondaryButton: {
    alignItems: 'center',
    borderColor: '#d0d7de',
    borderRadius: 8,
    borderWidth: 1,
    marginTop: 12,
    paddingVertical: 12,
  },
  secondaryButtonText: {
    color: '#24292f',
    fontSize: 15,
    fontWeight: '600',
  },
  primaryButton: {
    alignItems: 'center',
    backgroundColor: '#1f6feb',
    borderRadius: 8,
    marginTop: 24,
    paddingVertical: 14,
  },
  primaryButtonText: {
    color: '#ffffff',
    fontSize: 16,
    fontWeight: '600',
  },
  spinner: {
    marginTop: 24,
  },
  title: {
    color: '#24292f',
    fontSize: 24,
    fontWeight: '700',
    marginTop: 8,
  },
});

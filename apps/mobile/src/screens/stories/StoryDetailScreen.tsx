import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text } from 'react-native';

import { describeError } from '../../api/client';
import { Story, storiesApi } from '../../api/stories';

type StoryDetailScreenProps = {
  /** The id of the story to read. */
  readonly id: string;
  /** Called when the person returns to the feed. */
  readonly onBack: () => void;
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
}: StoryDetailScreenProps): React.ReactElement {
  const [story, setStory] = useState<Story | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const result = await storiesApi.getStory(id);
      setStory(result.story);
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
    marginTop: 20,
    paddingVertical: 12,
  },
  secondaryButtonText: {
    color: '#24292f',
    fontSize: 15,
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

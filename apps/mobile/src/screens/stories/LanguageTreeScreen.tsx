import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { describeError } from '../../api/client';
import { StoryVersion, versionDepths, versionsApi } from '../../api/versions';

type LanguageTreeScreenProps = {
  /** The story whose language tree to show. */
  readonly storyId: string;
  /** Called when the person returns to the story. */
  readonly onBack: () => void;
};

/** How far each level of the tree is indented, in points. */
const INDENT_PER_LEVEL = 16;

/** The first eight characters of an author id, so contributors are distinguishable. */
function authorPrefix(authorId: string): string {
  return authorId.slice(0, 8);
}

/**
 * The Language Tree: every version of a story, indented by how far it sits from
 * the root.
 *
 * The tree is rendered as an indented list on purpose. A graphical tree is a
 * later concern; what matters here is that a reader can see how one telling grew
 * out of another, and in which language.
 */
export default function LanguageTreeScreen({
  storyId,
  onBack,
}: LanguageTreeScreenProps): React.ReactElement {
  const [versions, setVersions] = useState<readonly StoryVersion[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const result = await versionsApi.getTree(storyId);
      setVersions(result.versions);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [storyId]);

  useEffect(() => {
    void load();
  }, [load]);

  const depths = versionDepths(versions);

  return (
    <ScrollView contentContainerStyle={styles.content}>
      <Pressable style={styles.link} onPress={onBack}>
        <Text style={styles.linkText}>Back to story</Text>
      </Pressable>

      <Text style={styles.title}>Language tree</Text>
      <Text style={styles.hint}>
        {versions.length} {versions.length === 1 ? 'version' : 'versions'} of this story
      </Text>

      {loading ? <ActivityIndicator style={styles.spinner} /> : null}
      {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

      {!loading && versions.length === 0 && error === undefined ? (
        <Text style={styles.hint}>This story has no versions yet.</Text>
      ) : null}

      {versions.map((version) => (
        <View
          key={version.id}
          style={[styles.row, { marginLeft: (depths.get(version.id) ?? 0) * INDENT_PER_LEVEL }]}
        >
          <Text style={styles.rowTitle}>{version.title}</Text>
          <View style={styles.rowMeta}>
            <Text style={styles.badge}>{version.language}</Text>
            <Text style={styles.author}>{authorPrefix(version.author_id)}</Text>
            {version.parent_version_id === null ? <Text style={styles.rootTag}>root</Text> : null}
          </View>
          {version.adaptation_note !== null && version.adaptation_note !== '' ? (
            <Text style={styles.note}>{version.adaptation_note}</Text>
          ) : null}
        </View>
      ))}

      {!loading && error !== undefined ? (
        <Pressable style={styles.secondaryButton} onPress={load}>
          <Text style={styles.secondaryButtonText}>Try again</Text>
        </Pressable>
      ) : null}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  author: {
    color: '#57606a',
    fontSize: 12,
    marginLeft: 8,
  },
  badge: {
    backgroundColor: '#eaeef2',
    borderRadius: 4,
    color: '#24292f',
    fontSize: 12,
    fontWeight: '600',
    paddingHorizontal: 6,
    paddingVertical: 2,
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
  hint: {
    color: '#57606a',
    fontSize: 13,
    marginTop: 6,
  },
  link: {
    marginBottom: 12,
  },
  linkText: {
    color: '#1f6feb',
    fontSize: 15,
  },
  note: {
    color: '#57606a',
    fontSize: 13,
    fontStyle: 'italic',
    marginTop: 6,
  },
  rootTag: {
    color: '#1f6feb',
    fontSize: 12,
    marginLeft: 8,
  },
  row: {
    borderColor: '#d0d7de',
    borderLeftWidth: 3,
    borderRadius: 6,
    borderWidth: 1,
    marginTop: 12,
    padding: 12,
  },
  rowMeta: {
    alignItems: 'center',
    flexDirection: 'row',
    marginTop: 6,
  },
  rowTitle: {
    color: '#24292f',
    fontSize: 16,
    fontWeight: '600',
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
  },
});

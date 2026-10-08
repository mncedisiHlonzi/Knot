import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { describeError } from '../../api/client';
import { StoryVersion, versionDepths, versionsApi } from '../../api/versions';
import RootedBadge from '../../components/RootedBadge';
import { colors, fontSizes, fontWeights, radius, spacing } from '../../theme';

type LanguageTreeScreenProps = {
  /** The story whose language tree to show. */
  readonly storyId: string;
  /** Called when the person returns to the story. */
  readonly onBack: () => void;
};

/** How far each level of the tree is indented, in points. */
const INDENT_PER_LEVEL = spacing.lg;

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
            {version.author_rooted ? (
              <View style={styles.rootedBadge}>
                <RootedBadge
                  place={version.author_rooted.place}
                  durationBucket={version.author_rooted.duration_bucket}
                  compact
                />
              </View>
            ) : null}
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
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginLeft: spacing.sm,
  },
  badge: {
    backgroundColor: colors.border.subtle,
    borderRadius: radius.sm,
    color: colors.text.primary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.semiBold,
    paddingHorizontal: 6,
    paddingVertical: 2,
  },
  content: {
    padding: spacing.xl,
    paddingBottom: spacing['3xl'],
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.base,
    marginTop: spacing.lg,
  },
  hint: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: 6,
  },
  link: {
    marginBottom: spacing.md,
  },
  linkText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
  },
  note: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    fontStyle: 'italic',
    marginTop: 6,
  },
  rootTag: {
    color: colors.text.brand,
    fontSize: fontSizes.sm,
    marginLeft: spacing.sm,
  },
  rootedBadge: {
    marginLeft: spacing.sm,
  },
  row: {
    borderColor: colors.border.default,
    borderLeftWidth: 3,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.md,
    padding: spacing.md,
  },
  rowMeta: {
    alignItems: 'center',
    flexDirection: 'row',
    marginTop: 6,
  },
  rowTitle: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    fontWeight: fontWeights.semiBold,
  },
  secondaryButton: {
    alignItems: 'center',
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: 20,
    paddingVertical: spacing.md,
  },
  secondaryButtonText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  spinner: {
    marginTop: spacing.xl,
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
  },
});

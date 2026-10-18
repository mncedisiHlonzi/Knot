import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { ArrowLeft } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { StoryVersion, versionsApi } from '../../api/versions';
import AuthorLine from '../../components/AuthorLine';
import Button from '../../components/Button';
import EmptyState from '../../components/EmptyState';
import Header from '../../components/Header';
import TheThread from '../../components/TheThread';
import { languageName } from '../../data/languages';
import { colors, layout, radius, spacing, typeScale } from '../../theme';
import { adaptCountLabel, treeRows, treeSummary } from './versionTree';

/** The Thread's length while the tree loads, matching a two-row preview. */
const TREE_THREAD_LENGTH = 112;

type LanguageTreeScreenProps = {
  /** The story whose language tree to show. */
  readonly storyId: string;
  /**
   * The signed-in user's access token, or undefined when signed out. A reaction
   * needs it; without it the bars invite the reader to sign in.
   */
  readonly token?: string;
  /** Called when a version's author is tapped, to open their profile. */
  readonly onOpenUserProfile: (userId: string) => void;
  /**
   * Called when a version row is tapped, to open that version's own detail. The
   * tree lists each version compactly; the detail is where its full body, note,
   * and reactions live.
   */
  readonly onOpenVersion: (versionId: string) => void;
  /**
   * Called when the reader wants to tell the story in their own language. It is
   * given the version to adapt from — the story's root. Omitted by a caller that
   * has nowhere to send the reader, in which case the action is not drawn.
   */
  readonly onTellStory?: (parentVersionId: string) => void;
  /** Called when the person returns to the story. */
  readonly onBack: () => void;
};

/** How far each level of the tree is indented, in points. */
const INDENT = spacing.xl;

/** The tree's connector line width and the diameter of its markers. */
const TREE_LINE = 1.5;
const DOT_SIZE = 12;

/** The elbow's reach: how far a child's connector curves out from its parent. */
const ELBOW = spacing.lg;

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
  token,
  onOpenUserProfile,
  onOpenVersion,
  onTellStory,
  onBack,
}: LanguageTreeScreenProps): React.ReactElement {
  const [versions, setVersions] = useState<readonly StoryVersion[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const result = await versionsApi.getTree(storyId, token);
      setVersions(result.versions);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [storyId, token]);

  useEffect(() => {
    void load();
  }, [load]);

  const rows = useMemo(() => treeRows(versions), [versions]);
  const summary = useMemo(() => treeSummary(rows), [rows]);
  const rootId = rows.find((row) => row.depth === 0)?.version.id;
  const tellStory = onTellStory;

  return (
    <View style={styles.container}>
      <Header
        title="Versions"
        left={
          <Pressable
            style={styles.iconButton}
            onPress={onBack}
            accessibilityRole="button"
            accessibilityLabel="Back"
          >
            <ArrowLeft color={colors.text.primary} size={24} />
          </Pressable>
        }
      />

      <View style={styles.summaryBlock}>
        <Text style={[typeScale.h3, styles.summaryTitle]} numberOfLines={2}>
          {summary.title}
        </Text>
        <Text style={[typeScale.bodyS, styles.summaryMeta]}>
          {summary.versionCount} {summary.versionCount === 1 ? 'version' : 'versions'} ·{' '}
          {summary.languageCount} {summary.languageCount === 1 ? 'language' : 'languages'}
        </Text>
      </View>

      <ScrollView contentContainerStyle={styles.content}>
        {/* The Thread runs down the tree's own spine while the versions load, so
            the wait already shows the shape it is about to fill (KNOT-020h). */}
        {loading ? (
          <View style={styles.spinner} testID="tree-thread">
            <TheThread orientation="vertical" length={TREE_THREAD_LENGTH} />
          </View>
        ) : null}
        {error !== undefined ? <Text style={[typeScale.bodyS, styles.error]}>{error}</Text> : null}

        {!loading && rows.length === 0 && error === undefined ? (
          <EmptyState
            illustrationName="no-stories"
            headline="No versions yet."
            body="Tell this story in your language and it will appear here beside the one it came from."
          />
        ) : null}

        {rows.map(({ version: item, depth, adaptCount }) => {
          const isRoot = depth === 0;
          const indent = depth * INDENT;
          const hasChildren = adaptCount > 0;

          return (
            <Pressable
              key={item.id}
              style={styles.row}
              onPress={() => onOpenVersion(item.id)}
              accessibilityRole="button"
              accessibilityLabel={`${languageName(item.language)} version`}
            >
              {/* The tree's gutter: a spine, an elbow, and the marker. */}
              <View style={[styles.gutter, { width: indent + DOT_SIZE + spacing.md }]}>
                {isRoot ? null : <View style={[styles.spineAbove, { left: indent - ELBOW }]} />}
                {isRoot ? null : <View style={[styles.elbow, { left: indent - ELBOW }]} />}
                {hasChildren ? (
                  <View style={[styles.spineBelow, { left: indent + DOT_SIZE / 2 }]} />
                ) : null}
                <View
                  style={[styles.dot, isRoot ? styles.dotRoot : styles.dotChild, { left: indent }]}
                />
              </View>

              <View style={styles.rowBody}>
                <View style={styles.languageRow}>
                  <Text style={[typeScale.button, styles.languageName]} numberOfLines={1}>
                    {languageName(item.language)}
                  </Text>
                  <Text style={[typeScale.caption, styles.languageCode]}>{item.language}</Text>
                </View>

                <View style={styles.metaRow}>
                  {/* A list row drops the Rooted badge: the tree is about
                      versions, not authorship. Detail views keep it. */}
                  <AuthorLine
                    displayName={item.author_display_name}
                    avatarUrl={item.author_avatar_url}
                    createdAt={item.created_at}
                    rooted={null}
                    size="small"
                    onPress={() => onOpenUserProfile(item.author_id)}
                  />
                  <Text style={[typeScale.caption, styles.adaptCount]}>
                    {adaptCountLabel(adaptCount)}
                  </Text>
                </View>

                {item.adaptation_note !== null && item.adaptation_note !== '' ? (
                  <Text style={[typeScale.bodyS, styles.note]}>{item.adaptation_note}</Text>
                ) : null}
              </View>
            </Pressable>
          );
        })}

        {!loading && error !== undefined ? (
          <View style={styles.retry}>
            <Button label="Try again" variant="secondary" onPress={() => void load()} fullWidth />
          </View>
        ) : null}
      </ScrollView>

      {tellStory !== undefined && rootId !== undefined ? (
        <View style={styles.footer}>
          <Button
            label="Tell this story in your language"
            variant="secondary"
            fullWidth
            onPress={() => tellStory(rootId)}
          />
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  adaptCount: {
    color: colors.text.tertiary,
  },
  container: {
    backgroundColor: colors.bg.canvas,
    flex: 1,
  },
  content: {
    paddingBottom: spacing.xxxl,
    paddingHorizontal: layout.screenPaddingReading,
  },
  // The marker, hollow for a child and filled for the story's root.
  dot: {
    borderRadius: radius.pill,
    borderWidth: 2,
    height: DOT_SIZE,
    marginTop: -DOT_SIZE / 2,
    position: 'absolute',
    top: '50%',
    width: DOT_SIZE,
  },
  dotChild: {
    backgroundColor: colors.bg.canvas,
    borderColor: colors.brand.primary,
  },
  dotRoot: {
    backgroundColor: colors.brand.primary,
    borderColor: colors.brand.primary,
  },
  // The curved corner that joins a child to its parent's spine.
  elbow: {
    borderBottomColor: colors.brand.tint,
    borderBottomLeftRadius: radius.lg,
    borderBottomWidth: TREE_LINE,
    borderLeftColor: colors.brand.tint,
    borderLeftWidth: TREE_LINE,
    height: ELBOW,
    marginTop: -ELBOW,
    position: 'absolute',
    top: '50%',
    width: ELBOW,
  },
  error: {
    color: colors.state.danger,
    marginTop: spacing.lg,
  },
  footer: {
    backgroundColor: colors.bg.canvas,
    borderTopColor: colors.border.hairline,
    borderTopWidth: 1,
    paddingBottom: spacing.xl,
    paddingHorizontal: layout.screenPaddingReading,
    paddingTop: spacing.md,
  },
  gutter: {
    flexShrink: 0,
  },
  iconButton: {
    alignItems: 'center',
    height: 44,
    justifyContent: 'center',
    width: 44,
  },
  languageCode: {
    color: colors.text.tertiary,
  },
  languageName: {
    color: colors.text.primary,
    flexShrink: 1,
  },
  languageRow: {
    alignItems: 'baseline',
    flexDirection: 'row',
    gap: spacing.sm,
  },
  metaRow: {
    alignItems: 'center',
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
    marginTop: spacing.xs,
  },
  note: {
    color: colors.text.secondary,
    fontStyle: 'italic',
    marginTop: spacing.sm,
  },
  retry: {
    marginTop: spacing.lg,
  },
  row: {
    flexDirection: 'row',
    paddingTop: spacing.lg,
  },
  rowBody: {
    flexShrink: 1,
    paddingBottom: spacing.lg,
  },
  // The spine that drops from above into a child's elbow.
  spineAbove: {
    borderLeftColor: colors.brand.tint,
    borderLeftWidth: TREE_LINE,
    height: '50%',
    position: 'absolute',
    top: 0,
  },
  // The spine that continues below a version that drew adaptations.
  spineBelow: {
    borderLeftColor: colors.brand.tint,
    borderLeftWidth: TREE_LINE,
    bottom: 0,
    position: 'absolute',
    top: '50%',
  },
  spinner: {
    alignItems: 'center',
    marginTop: spacing.xl,
  },
  summaryBlock: {
    borderBottomColor: colors.border.hairline,
    borderBottomWidth: 1,
    paddingBottom: spacing.lg,
    paddingHorizontal: layout.screenPaddingReading,
    paddingTop: spacing.sm,
  },
  summaryMeta: {
    color: colors.text.tertiary,
    marginTop: spacing.xs,
  },
  summaryTitle: {
    color: colors.text.primary,
  },
});

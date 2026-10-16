import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { ArrowLeft } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { EMPTY_REACTION_COUNTS } from '../../api/reactions';
import { StoryVersion, versionsApi } from '../../api/versions';
import AuthorLine from '../../components/AuthorLine';
import Button from '../../components/Button';
import Header from '../../components/Header';
import ReactionBar from '../../components/ReactionBar';
import { languageName } from '../../data/languages';
import { colors, layout, radius, spacing, typeScale } from '../../theme';

type StoryVersionScreenProps = {
  /** The id of the version to read. */
  readonly versionId: string;
  /**
   * The signed-in user's access token, or undefined when signed out. A reaction
   * needs it; without it the bar invites the reader to sign in.
   */
  readonly token?: string;
  /** Called when the person returns to the tree (or wherever they came from). */
  readonly onBack: () => void;
  /**
   * Called to adapt this version. It is given the version's story and its own id:
   * a version may be adapted from at any point in the tree, so the version itself
   * is the parent of the new telling.
   */
  readonly onAdapt: (storyId: string, parentVersionId: string) => void;
  /** Called when the person wants to read the story the version belongs to. */
  readonly onOpenStory: (storyId: string) => void;
  /** Called when the version's author is tapped, to open their profile. */
  readonly onOpenUserProfile: (userId: string) => void;
};

/**
 * One version of a story, read on its own.
 *
 * The Language Tree lists versions compactly; this screen is the detail view a row
 * opens onto: the version's language and its ISO code, the author's attribution
 * (with the Rooted badge — a detail view keeps it, unlike a list row), the title,
 * the body, the adapter's note when one was left, the four perspective signals,
 * and the two actions a version invites — adapt it, or read the story it belongs
 * to.
 *
 * The structure mirrors `StoryDetailScreen` so a reader moving between a story and
 * one of its versions finds the same reading column and the same reaction bar. It
 * fetches through the same `versionsApi` client and reports a missing or blocked
 * version as a readable message rather than a crash, because `describeError` names
 * the server's 404 verbatim.
 */
export default function StoryVersionScreen({
  versionId,
  token,
  onBack,
  onAdapt,
  onOpenStory,
  onOpenUserProfile,
}: StoryVersionScreenProps): React.ReactElement {
  const [version, setVersion] = useState<StoryVersion | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const result = await versionsApi.getVersion(versionId, token);
      setVersion(result.version);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [token, versionId]);

  useEffect(() => {
    void load();
  }, [load]);

  const adaptationNote = (version?.adaptation_note ?? '').trim();
  const attributionLine = (version?.attribution_line ?? '').trim();

  return (
    <View style={styles.container}>
      <Header
        title="Version"
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

      <ScrollView contentContainerStyle={styles.content}>
        {loading ? (
          <View style={styles.loadingRow}>
            <ActivityIndicator />
          </View>
        ) : null}

        {error !== undefined ? <Text style={[typeScale.bodyS, styles.error]}>{error}</Text> : null}

        {version !== undefined ? (
          <>
            <View style={styles.languageRow}>
              <Text style={[typeScale.button, styles.languageName]} numberOfLines={1}>
                {languageName(version.language)}
              </Text>
              <Text style={[typeScale.caption, styles.languageCode]}>{version.language}</Text>
            </View>

            <View style={styles.authorRow}>
              {/* A detail view keeps the author's Rooted badge — only list views
                  (the Language Tree's rows, a comment's rows) drop it. */}
              <AuthorLine
                displayName={version.author_display_name}
                avatarUrl={version.author_avatar_url}
                createdAt={version.created_at}
                rooted={version.author_rooted}
                size="medium"
                onPress={() => onOpenUserProfile(version.author_id)}
              />
            </View>

            {/* Who this version was adapted from, in whose language, and from
                whom in turn. The server renders the sentence; this screen shows
                it verbatim and renders nothing when it is absent, so an older
                server does not leave a blank line (KNOT-021). */}
            {attributionLine === '' ? null : (
              <Text style={[typeScale.caption, styles.attribution]} numberOfLines={2}>
                {attributionLine}
              </Text>
            )}

            <Text style={[typeScale.h1, styles.title]}>{version.title}</Text>

            <Text style={[typeScale.reading, styles.body]}>{version.body}</Text>

            {adaptationNote !== '' ? (
              <View style={styles.noteBlock}>
                <Text style={[typeScale.overline, styles.noteLabel]}>Adaptation note</Text>
                <Text style={[typeScale.bodyS, styles.noteBody]}>{adaptationNote}</Text>
              </View>
            ) : null}

            <View style={styles.hairline} />

            <ReactionBar
              entityType="version"
              entityId={version.id}
              counts={version.reactions ?? EMPTY_REACTION_COUNTS}
              myReactions={version.my_reactions ?? []}
              token={token}
              onChange={(counts, mine) =>
                setVersion((previous) =>
                  previous === undefined
                    ? previous
                    : { ...previous, reactions: counts, my_reactions: mine },
                )
              }
            />

            <View style={styles.actions}>
              <Button
                variant="primary"
                fullWidth
                label="Adapt this version"
                onPress={() => onAdapt(version.story_id, version.id)}
              />
              <View style={styles.actionGap} />
              <Button
                variant="ghost"
                fullWidth
                label="View the full story"
                onPress={() => onOpenStory(version.story_id)}
              />
            </View>
          </>
        ) : null}

        {!loading && version === undefined && error !== undefined ? (
          <View style={styles.retry}>
            <Button variant="secondary" label="Try again" onPress={() => void load()} />
          </View>
        ) : null}
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  actionGap: {
    height: spacing.sm,
  },
  actions: {
    marginTop: spacing.lg,
  },
  // The lineage sentence under the byline: quiet enough to read as a note rather
  // than as a second headline, and separate from the author block so it never
  // competes with the person's name (KNOT-021).
  attribution: {
    color: colors.text.tertiary,
    marginTop: spacing.sm,
  },
  authorRow: {
    marginTop: spacing.sm,
  },
  body: {
    color: colors.text.primary,
    marginTop: spacing.lg,
  },
  container: {
    backgroundColor: colors.bg.canvas,
    flex: 1,
  },
  content: {
    paddingBottom: spacing.xxxl,
    paddingHorizontal: layout.screenPaddingReading,
    paddingTop: spacing.sm,
  },
  error: {
    color: colors.state.danger,
    marginTop: spacing.lg,
  },
  hairline: {
    backgroundColor: colors.border.hairline,
    height: 1,
    marginTop: spacing.xl,
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
  loadingRow: {
    alignItems: 'center',
    paddingTop: spacing.xxl,
  },
  noteBlock: {
    backgroundColor: colors.bg.paper,
    borderColor: colors.border.hairline,
    borderRadius: radius.lg,
    borderWidth: 1,
    marginTop: spacing.lg,
    padding: spacing.md,
  },
  noteBody: {
    color: colors.text.secondary,
    marginTop: spacing.xs,
  },
  noteLabel: {
    color: colors.text.tertiary,
  },
  retry: {
    marginTop: spacing.xl,
  },
  title: {
    color: colors.text.primary,
    marginTop: spacing.md,
  },
});

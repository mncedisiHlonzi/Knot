import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import { describeError } from '../../api/client';
import { Comment, THREAD_PAGE_SIZE, conversationsApi } from '../../api/conversations';
import AuthorLine from '../../components/AuthorLine';
import { isLanguageCode, languageName } from '../../data/languages';
import { colors, fontSizes, fontWeights, lineHeights, radius, spacing } from '../../theme';

type CommentThreadScreenProps = {
  /** The version whose conversation to show. */
  readonly versionId: string;
  /** The signed-in user's access token, needed to post a comment. */
  readonly token: string;
  /** The language the comment box starts with, e.g. the user's first preferred tag. */
  readonly language?: string;
  /** Called when the person wants to bridge a comment into another language. */
  readonly onBridge: (comment: Comment) => void;
  /** Called when a commenter is tapped, to open their profile. */
  readonly onOpenUserProfile: (userId: string) => void;
  /** Called when the person returns to the story. */
  readonly onBack: () => void;
};

/** Field limits, mirroring the server's rules so the user is told early. */
const MAX_BODY_LENGTH = 5000;

/** The language the comment box falls back to when the caller suggests nothing usable. */
const DEFAULT_COMPOSE_LANGUAGE = 'eng';

/**
 * Returns the first client-side validation problem, or undefined when the form is
 * acceptable. The server validates again and remains the source of truth.
 */
function validateForm(body: string, language: string): string | undefined {
  if (body.trim() === '') {
    return 'A comment is required.';
  }
  if (body.length > MAX_BODY_LENGTH) {
    return `The comment must be at most ${MAX_BODY_LENGTH} characters.`;
  }
  if (!isLanguageCode(language)) {
    return 'Use a language code from the list, such as en or zu.';
  }
  return undefined;
}

/**
 * One version's conversation: a flat list of comments, newest first, with a
 * comment box at the bottom.
 *
 * Every comment carries a "Bridge to another language" action. A bridge is how
 * someone replies in their own language: it writes a new comment in the target
 * language and joins it to the one it came from, leaving this conversation
 * intact (see KNOT-ADR-014).
 */
export default function CommentThreadScreen({
  versionId,
  token,
  language,
  onBridge,
  onOpenUserProfile,
  onBack,
}: CommentThreadScreenProps): React.ReactElement {
  const [comments, setComments] = useState<readonly Comment[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const [body, setBody] = useState('');
  const [composeLanguage, setComposeLanguage] = useState(() => {
    const preferred = (language ?? '').trim();
    return isLanguageCode(preferred) ? preferred : DEFAULT_COMPOSE_LANGUAGE;
  });
  const [composeError, setComposeError] = useState<string | undefined>(undefined);
  const [posting, setPosting] = useState(false);

  const loadFirstPage = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const page = await conversationsApi.listComments(versionId, { limit: THREAD_PAGE_SIZE });
      setComments(page.comments);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [versionId]);

  useEffect(() => {
    void loadFirstPage();
  }, [loadFirstPage]);

  const refresh = useCallback(async (): Promise<void> => {
    setRefreshing(true);

    try {
      const page = await conversationsApi.listComments(versionId, { limit: THREAD_PAGE_SIZE });
      setComments(page.comments);
      setNextCursor(page.next_cursor);
      setError(undefined);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setRefreshing(false);
    }
  }, [versionId]);

  const loadMore = useCallback(async (): Promise<void> => {
    if (nextCursor === '' || loadingMore) {
      return;
    }

    setLoadingMore(true);

    try {
      const page = await conversationsApi.listComments(versionId, {
        cursor: nextCursor,
        limit: THREAD_PAGE_SIZE,
      });
      // Append rather than replace: the cursor is exclusive, so a page never
      // repeats a comment already on screen.
      setComments((existing) => [...existing, ...page.comments]);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoadingMore(false);
    }
  }, [loadingMore, nextCursor, versionId]);

  async function handlePost(): Promise<void> {
    const problem = validateForm(body, composeLanguage);
    if (problem !== undefined) {
      setComposeError(problem);
      return;
    }

    setComposeError(undefined);
    setPosting(true);

    try {
      await conversationsApi.createComment(versionId, token, {
        body,
        language: composeLanguage,
      });
      setBody('');
      await refresh();
    } catch (caught) {
      setComposeError(describeError(caught));
    } finally {
      setPosting(false);
    }
  }

  return (
    <View style={styles.container}>
      <View style={styles.header}>
        <Pressable style={styles.link} onPress={onBack}>
          <Text style={styles.linkText}>← Back</Text>
        </Pressable>
        <Text style={styles.title}>Conversation</Text>
        <Text style={styles.hint}>
          {comments.length} {comments.length === 1 ? 'comment' : 'comments'}
        </Text>
        {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}
      </View>

      {loading ? <ActivityIndicator style={styles.spinner} /> : null}

      <FlatList
        data={comments}
        keyExtractor={(comment) => comment.id}
        contentContainerStyle={styles.list}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={refresh} />}
        renderItem={({ item }) => (
          <View style={styles.card}>
            <View style={styles.cardMeta}>
              <Text style={styles.badge}>{languageName(item.language)}</Text>
              <AuthorLine
                displayName={item.author_display_name}
                avatarUrl={item.author_avatar_url}
                createdAt={item.created_at}
                rooted={item.author_rooted}
                size="small"
                onPress={() => onOpenUserProfile(item.author_id)}
              />
            </View>
            <Text style={styles.body}>{item.body}</Text>
            <Pressable style={styles.bridgeButton} onPress={() => onBridge(item)}>
              <Text style={styles.bridgeButtonText}>Bridge to another language</Text>
            </Pressable>
          </View>
        )}
        ListEmptyComponent={
          !loading && error === undefined ? (
            <Text style={styles.hint}>No comments yet. Start the conversation.</Text>
          ) : null
        }
        ListFooterComponent={
          nextCursor !== '' ? (
            <Pressable
              style={[styles.secondaryButton, loadingMore ? styles.disabled : null]}
              onPress={loadMore}
              disabled={loadingMore}
            >
              <Text style={styles.secondaryButtonText}>
                {loadingMore ? 'Loading…' : 'Load more'}
              </Text>
            </Pressable>
          ) : null
        }
      />

      <View style={styles.composer}>
        <TextInput
          style={styles.composerInput}
          value={body}
          onChangeText={setBody}
          placeholder="Add a comment"
          placeholderTextColor={colors.text.secondary}
          multiline
          numberOfLines={3}
          textAlignVertical="top"
        />
        <View style={styles.composerRow}>
          <TextInput
            style={styles.languageInput}
            value={composeLanguage}
            onChangeText={setComposeLanguage}
            autoCapitalize="none"
            autoCorrect={false}
            placeholder="eng"
            placeholderTextColor={colors.text.secondary}
            accessibilityLabel="Comment language code"
          />
          <Pressable
            style={[styles.postButton, posting ? styles.disabled : null]}
            onPress={handlePost}
            disabled={posting}
          >
            <Text style={styles.postButtonText}>{posting ? 'Posting…' : 'Post'}</Text>
          </Pressable>
        </View>
        {composeError !== undefined ? <Text style={styles.error}>{composeError}</Text> : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  badge: {
    backgroundColor: colors.border.subtle,
    borderRadius: radius.sm,
    color: colors.text.primary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.semiBold,
    paddingHorizontal: 6,
    paddingVertical: 2,
  },
  body: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    lineHeight: lineHeights.base,
    marginTop: spacing.sm,
  },
  bridgeButton: {
    alignSelf: 'flex-start',
    marginTop: 10,
  },
  bridgeButtonText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
  },
  card: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.subtle,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.md,
    padding: spacing.lg,
  },
  cardMeta: {
    alignItems: 'center',
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
  },
  composer: {
    borderTopColor: colors.border.default,
    borderTopWidth: 1,
    padding: spacing.lg,
  },
  composerInput: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    color: colors.text.primary,
    fontSize: fontSizes.base,
    minHeight: 64,
    paddingHorizontal: spacing.md,
    paddingVertical: 10,
  },
  composerRow: {
    flexDirection: 'row',
    marginTop: 10,
  },
  container: {
    backgroundColor: colors.bg.primary,
    flex: 1,
  },
  disabled: {
    opacity: 0.5,
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.base,
    marginTop: 10,
  },
  header: {
    padding: spacing.xl,
    paddingBottom: 0,
  },
  hint: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: 6,
  },
  languageInput: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    color: colors.text.primary,
    fontSize: fontSizes.base,
    paddingHorizontal: spacing.md,
    paddingVertical: 10,
    width: 80,
  },
  link: {
    marginBottom: spacing.md,
  },
  linkText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
  },
  list: {
    paddingHorizontal: spacing.xl,
    paddingBottom: spacing.xl,
  },
  postButton: {
    alignItems: 'center',
    backgroundColor: colors.brand.purple,
    borderRadius: radius.md,
    flexGrow: 1,
    justifyContent: 'center',
    marginLeft: 10,
    paddingVertical: spacing.md,
  },
  postButtonText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
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
  spinner: {
    marginTop: spacing.xl,
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
  },
});

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

type CommentThreadScreenProps = {
  /** The version whose conversation to show. */
  readonly versionId: string;
  /** The signed-in user's access token, needed to post a comment. */
  readonly token: string;
  /** The language the comment box starts with, e.g. the user's first preferred tag. */
  readonly language?: string;
  /** Called when the person wants to bridge a comment into another language. */
  readonly onBridge: (comment: Comment) => void;
  /** Called when the person returns to the story. */
  readonly onBack: () => void;
};

/** Field limits, mirroring the server's rules so the user is told early. */
const MAX_BODY_LENGTH = 5000;
const LANGUAGE_PATTERN = /^[A-Za-z]{2,8}$/;

/** The first eight characters of an author id, so commenters are distinguishable. */
function authorPrefix(authorId: string): string {
  return authorId.slice(0, 8);
}

/**
 * Renders a comment's timestamp as a short, locale-independent date and time.
 * An unparseable timestamp is shown verbatim rather than as "Invalid Date".
 */
function formatDate(value: string): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toISOString().slice(0, 16).replace('T', ' ');
}

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
  if (!LANGUAGE_PATTERN.test(language.trim())) {
    return 'The language must be 2 to 8 letters, such as en or fr.';
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
    return preferred === '' ? 'en' : preferred;
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
        language: composeLanguage.trim().toLowerCase(),
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
          <Text style={styles.linkText}>Back to story</Text>
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
              <Text style={styles.badge}>{item.language}</Text>
              <Text style={styles.author}>{authorPrefix(item.author_id)}</Text>
              <Text style={styles.date}>{formatDate(item.created_at)}</Text>
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
          placeholderTextColor="#9aa0a6"
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
            placeholder="en"
            placeholderTextColor="#9aa0a6"
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
  body: {
    color: '#24292f',
    fontSize: 15,
    lineHeight: 22,
    marginTop: 8,
  },
  bridgeButton: {
    alignSelf: 'flex-start',
    marginTop: 10,
  },
  bridgeButtonText: {
    color: '#1f6feb',
    fontSize: 14,
  },
  card: {
    borderColor: '#d0d7de',
    borderRadius: 8,
    borderWidth: 1,
    marginTop: 12,
    padding: 14,
  },
  cardMeta: {
    alignItems: 'center',
    flexDirection: 'row',
  },
  composer: {
    borderTopColor: '#d0d7de',
    borderTopWidth: 1,
    padding: 16,
  },
  composerInput: {
    borderColor: '#d0d7de',
    borderRadius: 8,
    borderWidth: 1,
    color: '#24292f',
    fontSize: 15,
    minHeight: 64,
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  composerRow: {
    flexDirection: 'row',
    marginTop: 10,
  },
  container: {
    flex: 1,
  },
  date: {
    color: '#57606a',
    fontSize: 12,
    marginLeft: 8,
  },
  disabled: {
    opacity: 0.5,
  },
  error: {
    color: '#b3261e',
    fontSize: 14,
    marginTop: 10,
  },
  header: {
    padding: 24,
    paddingBottom: 0,
  },
  hint: {
    color: '#57606a',
    fontSize: 13,
    marginTop: 6,
  },
  languageInput: {
    borderColor: '#d0d7de',
    borderRadius: 8,
    borderWidth: 1,
    color: '#24292f',
    fontSize: 15,
    paddingHorizontal: 12,
    paddingVertical: 10,
    width: 80,
  },
  link: {
    marginBottom: 12,
  },
  linkText: {
    color: '#1f6feb',
    fontSize: 15,
  },
  list: {
    paddingHorizontal: 24,
    paddingBottom: 24,
  },
  postButton: {
    alignItems: 'center',
    backgroundColor: '#1f6feb',
    borderRadius: 8,
    flexGrow: 1,
    justifyContent: 'center',
    marginLeft: 10,
    paddingVertical: 12,
  },
  postButtonText: {
    color: '#ffffff',
    fontSize: 15,
    fontWeight: '600',
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
  spinner: {
    marginTop: 24,
  },
  title: {
    color: '#24292f',
    fontSize: 24,
    fontWeight: '700',
  },
});

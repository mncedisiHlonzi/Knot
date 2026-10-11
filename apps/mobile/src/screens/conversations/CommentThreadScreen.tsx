import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Modal,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import { describeError } from '../../api/client';
import {
  Comment,
  CreateCommentPayload,
  THREAD_PAGE_SIZE,
  conversationsApi,
} from '../../api/conversations';
import { versionsApi } from '../../api/versions';
import AuthorLine from '../../components/AuthorLine';
import LanguagePicker from '../../components/LanguagePicker';
import ReportAction from '../../components/ReportAction';
import { isLanguageCode, languageName } from '../../data/languages';
import { colors, fontSizes, fontWeights, lineHeights, radius, spacing } from '../../theme';
import {
  DEFAULT_COMPOSE_LANGUAGE,
  REPLIES_PREVIEW_COUNT,
  groupComments,
  initialComposeLanguage,
  languageChipLabel,
  repliesLinkLabel,
  replyTargetId,
} from './commentThread';

type CommentThreadScreenProps = {
  /** The version whose conversation to show. */
  readonly versionId: string;
  /** The signed-in user's access token, needed to post a comment. */
  readonly token: string;
  /**
   * The user's first preferred language, which the composer starts in. When the
   * user has none, the composer falls back to the version's own language, which
   * it fetches, and then to English.
   */
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
    return 'Choose a language for the comment, such as eng or zul.';
  }
  return undefined;
}

/**
 * One version's conversation: the top-level comments, newest first, each with its
 * replies revealed inline on demand, and a comment box at the bottom.
 *
 * Threading is one level deep (KNOT-ADR-047). Replies are hidden behind a link
 * under their parent and expand in place (KNOT-ADR-049): the first tap shows the
 * first few, a second tap shows the rest, and the next tap collapses them again.
 * A collapsed comment never pushes its neighbours off the screen, however many
 * replies it gathers. The "Reply" action on a reply answers the same top-level
 * comment, so the thread never nests further than that.
 *
 * Every comment carries a "Bridge to another language" action. A bridge is how
 * someone replies in their own language: it writes a new comment in the target
 * language and joins it to the one it came from, leaving this conversation
 * intact (see KNOT-ADR-014).
 *
 * The comment box's language is shown as a chip reading the language's name, and
 * tapping it opens a picker. The choice lasts for this visit only: it is not
 * written back to the user's profile (KNOT-ADR-048).
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

  // The caller's suggestion is the user's first preferred language. When there
  // is none, the composer starts undecided and resolves to the version's
  // language below.
  const suggestedLanguage = (language ?? '').trim();
  const hasSuggestedLanguage = isLanguageCode(suggestedLanguage);

  const [body, setBody] = useState('');
  const [composeLanguage, setComposeLanguage] = useState(() =>
    hasSuggestedLanguage ? suggestedLanguage : DEFAULT_COMPOSE_LANGUAGE,
  );
  // True while the version's language is still being fetched. An empty chip would
  // read as "no language"; "…" says the answer is on its way.
  const [resolvingLanguage, setResolvingLanguage] = useState(!hasSuggestedLanguage);
  const [pickerOpen, setPickerOpen] = useState(false);
  // The comment being answered, or undefined when writing a top-level comment.
  const [replyTo, setReplyTo] = useState<Comment | undefined>(undefined);
  const [composeError, setComposeError] = useState<string | undefined>(undefined);
  const [posting, setPosting] = useState(false);

  // Which comments show their replies, and which show every reply rather than the
  // first few. Both are session-only and per comment: leaving the thread forgets
  // them (KNOT-ADR-049). Each change makes a new Set so React sees a new value.
  const [expandedIds, setExpandedIds] = useState<Set<string>>(() => new Set());
  const [fullyExpandedIds, setFullyExpandedIds] = useState<Set<string>>(() => new Set());

  // The composer, so a Reply tap can put the cursor in it (KNOT-015e-fix).
  const composerRef = useRef<TextInput>(null);

  useEffect(() => {
    if (hasSuggestedLanguage) {
      return;
    }

    let cancelled = false;

    async function resolveVersionLanguage(): Promise<void> {
      try {
        const { version } = await versionsApi.getVersion(versionId);
        if (!cancelled) {
          setComposeLanguage(initialComposeLanguage(undefined, version.language));
        }
      } catch {
        // Silent: English is already selected, and the chip still lets the user
        // choose. Failing here must not block writing a comment.
      } finally {
        if (!cancelled) {
          setResolvingLanguage(false);
        }
      }
    }

    void resolveVersionLanguage();

    return () => {
      cancelled = true;
    };
  }, [hasSuggestedLanguage, versionId]);

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

  // The list renders top-level comments only; each one owns its replies, which it
  // reveals inline when expanded. The order the API returned is preserved.
  const { topLevel, repliesByParent } = useMemo(() => groupComments(comments), [comments]);

  // Tapping the replies link moves a comment through collapsed → the first few →
  // all → collapsed. `total` decides whether a second tap is needed at all.
  function handleRepliesLinkPress(commentId: string, total: number): void {
    const expanded = expandedIds.has(commentId);
    const fullyExpanded = fullyExpandedIds.has(commentId);

    if (!expanded) {
      setExpandedIds((previous) => {
        const next = new Set(previous);
        next.add(commentId);
        return next;
      });
      return;
    }

    if (!fullyExpanded && total > REPLIES_PREVIEW_COUNT) {
      setFullyExpandedIds((previous) => {
        const next = new Set(previous);
        next.add(commentId);
        return next;
      });
      return;
    }

    setExpandedIds((previous) => {
      const next = new Set(previous);
      next.delete(commentId);
      return next;
    });
    setFullyExpandedIds((previous) => {
      const next = new Set(previous);
      next.delete(commentId);
      return next;
    });
  }

  // Tapping Reply targets the comment the user tapped — a reply keeps that reply's
  // author in the banner — while the payload names the top-level comment through
  // `replyTargetId`. The composer is focused so the keyboard opens ready to type.
  function handleReplyPress(comment: Comment): void {
    setReplyTo(comment);
    composerRef.current?.focus();
  }

  async function handlePost(): Promise<void> {
    const problem = validateForm(body, composeLanguage);
    if (problem !== undefined) {
      setComposeError(problem);
      return;
    }

    setComposeError(undefined);
    setPosting(true);

    // A reply names the comment it answers. `replyTargetId` resolves a reply to
    // its top-level comment, matching the server's depth limit.
    const payload: CreateCommentPayload =
      replyTo === undefined
        ? { body, language: composeLanguage }
        : { body, language: composeLanguage, parent_comment_id: replyTargetId(replyTo) };

    try {
      await conversationsApi.createComment(versionId, token, payload);
      setBody('');
      setReplyTo(undefined);
      await refresh();
    } catch (caught) {
      setComposeError(describeError(caught));
    } finally {
      setPosting(false);
    }
  }

  const replyToName = (replyTo?.author_display_name ?? '').trim();
  const chipLabel = resolvingLanguage ? '…' : languageChipLabel(composeLanguage);

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
        data={topLevel}
        keyExtractor={(comment) => comment.id}
        contentContainerStyle={styles.list}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={refresh} />}
        renderItem={({ item }) => {
          const replies = repliesByParent[item.id] ?? [];
          const expanded = expandedIds.has(item.id);
          const fullyExpanded = fullyExpandedIds.has(item.id);
          const linkLabel = repliesLinkLabel(replies.length, expanded, fullyExpanded);
          // Collapsed shows none; the first tap shows the first few; the second
          // tap (fully expanded) shows them all.
          const visibleReplies = expanded
            ? fullyExpanded
              ? replies
              : replies.slice(0, REPLIES_PREVIEW_COUNT)
            : [];

          return (
            <View style={styles.card}>
              <View style={styles.cardMeta}>
                <Text style={styles.badge}>{languageName(item.language)}</Text>
                <AuthorLine
                  displayName={item.author_display_name}
                  avatarUrl={item.author_avatar_url}
                  createdAt={item.created_at}
                  rooted={item.author_rooted}
                  size="medium"
                  onPress={() => onOpenUserProfile(item.author_id)}
                />
              </View>
              <Text style={styles.body}>{item.body}</Text>
              {linkLabel !== null ? (
                <Pressable
                  style={styles.repliesLink}
                  onPress={() => handleRepliesLinkPress(item.id, replies.length)}
                  accessibilityRole="button"
                >
                  <Text style={styles.repliesLinkText}>{linkLabel}</Text>
                </Pressable>
              ) : null}
              <View style={styles.cardActions}>
                <Pressable style={styles.action} onPress={() => handleReplyPress(item)}>
                  <Text style={styles.actionText}>Reply</Text>
                </Pressable>
                <Pressable style={styles.action} onPress={() => onBridge(item)}>
                  <Text style={styles.actionText}>Bridge to another language</Text>
                </Pressable>
                <ReportAction token={token} entityType="comment" entityId={item.id} />
              </View>
              {visibleReplies.map((reply) => (
                <View key={reply.id} style={styles.replyRow}>
                  <AuthorLine
                    displayName={reply.author_display_name}
                    avatarUrl={reply.author_avatar_url}
                    createdAt={reply.created_at}
                    rooted={reply.author_rooted}
                    size="small"
                    onPress={() => onOpenUserProfile(reply.author_id)}
                  />
                  <Text style={styles.replyBody}>{reply.body}</Text>
                  <View style={styles.replyActions}>
                    <Pressable style={styles.replyAction} onPress={() => handleReplyPress(reply)}>
                      <Text style={styles.actionText}>Reply</Text>
                    </Pressable>
                    <Pressable style={styles.replyAction} onPress={() => onBridge(reply)}>
                      <Text style={styles.actionText}>Bridge to another language</Text>
                    </Pressable>
                    <ReportAction token={token} entityType="comment" entityId={reply.id} />
                  </View>
                </View>
              ))}
            </View>
          );
        }}
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
        {replyTo !== undefined ? (
          <View style={styles.replyBanner}>
            <Text style={styles.replyBannerText} numberOfLines={1}>
              Replying to {replyToName === '' ? 'this commenter' : replyToName}
            </Text>
            <Pressable
              style={styles.replyBannerCancel}
              onPress={() => setReplyTo(undefined)}
              accessibilityLabel="Cancel the reply"
              accessibilityRole="button"
            >
              <Text style={styles.replyBannerCancelText}>✕</Text>
            </Pressable>
          </View>
        ) : null}
        <TextInput
          ref={composerRef}
          style={styles.composerInput}
          value={body}
          onChangeText={setBody}
          placeholder={replyTo === undefined ? 'Add a comment' : 'Write a reply'}
          placeholderTextColor={colors.text.secondary}
          multiline
          numberOfLines={3}
          textAlignVertical="top"
        />
        <View style={styles.composerRow}>
          <Pressable
            style={styles.languageChip}
            onPress={() => setPickerOpen(true)}
            accessibilityRole="button"
            accessibilityLabel="Comment language"
          >
            <Text style={styles.languageChipText} numberOfLines={1}>
              {chipLabel === '' ? 'Choose a language' : chipLabel}
            </Text>
            <Text style={styles.languageChipChevron}>▾</Text>
          </Pressable>
          <Pressable
            style={[styles.postButton, posting ? styles.disabled : null]}
            onPress={handlePost}
            disabled={posting}
          >
            <Text style={styles.postButtonText}>
              {posting ? 'Posting…' : replyTo === undefined ? 'Post' : 'Reply'}
            </Text>
          </Pressable>
        </View>
        {composeError !== undefined ? <Text style={styles.error}>{composeError}</Text> : null}
      </View>

      <Modal
        visible={pickerOpen}
        transparent
        animationType="slide"
        onRequestClose={() => setPickerOpen(false)}
      >
        <Pressable style={styles.modalBackdrop} onPress={() => setPickerOpen(false)}>
          {/* The sheet swallows taps so pressing inside it cannot dismiss it. */}
          <Pressable style={styles.modalSheet} onPress={() => undefined}>
            <View style={styles.modalHeader}>
              <Text style={styles.modalTitle}>Comment language</Text>
              <Pressable
                style={styles.modalDone}
                onPress={() => setPickerOpen(false)}
                accessibilityRole="button"
                accessibilityLabel="Close the language picker"
              >
                <Text style={styles.linkText}>Done</Text>
              </Pressable>
            </View>
            <Text style={styles.hint}>
              This only changes the language of this comment, not your profile.
            </Text>
            <LanguagePicker
              mode="single"
              selected={composeLanguage}
              onSelect={(chosen) => {
                setComposeLanguage(chosen.code);
                setPickerOpen(false);
              }}
              onClear={() => setComposeLanguage('')}
              placeholder="Search languages"
            />
          </Pressable>
        </Pressable>
      </Modal>
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
  action: {
    marginRight: spacing.lg,
    marginTop: 10,
  },
  actionText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
  },
  cardActions: {
    flexDirection: 'row',
    flexWrap: 'wrap',
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
  languageChip: {
    alignItems: 'center',
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.pill,
    borderWidth: 1,
    flexDirection: 'row',
    paddingHorizontal: spacing.md,
    paddingVertical: 10,
  },
  languageChipChevron: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginLeft: 6,
  },
  languageChipText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    maxWidth: 140,
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
  modalBackdrop: {
    backgroundColor: 'rgba(0, 0, 0, 0.6)',
    flex: 1,
    justifyContent: 'flex-end',
  },
  modalDone: {
    marginLeft: spacing.md,
  },
  modalHeader: {
    alignItems: 'center',
    flexDirection: 'row',
    justifyContent: 'space-between',
  },
  modalSheet: {
    backgroundColor: colors.bg.primary,
    borderTopLeftRadius: radius.xl,
    borderTopRightRadius: radius.xl,
    maxHeight: '80%',
    padding: spacing.xl,
  },
  modalTitle: {
    color: colors.text.primary,
    fontSize: fontSizes.lg,
    fontWeight: fontWeights.bold,
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
  replyBanner: {
    alignItems: 'center',
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: 10,
  },
  replyBannerCancel: {
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
  },
  replyBannerCancelText: {
    color: colors.text.secondary,
    fontSize: fontSizes.base,
  },
  replyBannerText: {
    color: colors.text.secondary,
    flexShrink: 1,
    fontSize: fontSizes.sm,
  },
  // A reply sits inside its parent's card, indented to show the hierarchy. The
  // indent is inside the card, so the thread reads as one unit and the reply is
  // shallower than the old nested card (KNOT-ADR-049).
  replyRow: {
    borderTopColor: colors.border.subtle,
    borderTopWidth: 1,
    marginLeft: spacing.xl,
    marginTop: spacing.md,
    paddingTop: spacing.md,
  },
  replyBody: {
    color: colors.text.primary,
    fontSize: fontSizes.sm,
    lineHeight: lineHeights.sm,
    marginTop: spacing.xs,
  },
  replyActions: {
    flexDirection: 'row',
    flexWrap: 'wrap',
  },
  replyAction: {
    marginRight: spacing.lg,
    marginTop: spacing.sm,
  },
  // The expand/collapse link sits above the action buttons, so the replies it
  // reveals appear below the actions that address the comment itself.
  repliesLink: {
    marginTop: spacing.sm,
  },
  repliesLinkText: {
    color: colors.text.brand,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.medium,
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

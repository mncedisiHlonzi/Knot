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
import { ArrowLeft } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import {
  Comment,
  CreateCommentPayload,
  THREAD_PAGE_SIZE,
  conversationsApi,
} from '../../api/conversations';
import { versionsApi } from '../../api/versions';
import AuthorLine from '../../components/AuthorLine';
import Button from '../../components/Button';
import EmptyState from '../../components/EmptyState';
import Header from '../../components/Header';
import LanguagePicker from '../../components/LanguagePicker';
import { isLanguageCode } from '../../data/languages';
import { layout, radius, spacing, typeScale, useTheme, useThemedStyles } from '../../theme';
import type { ThemeColors } from '../../theme';
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
 * Every comment can be replied to, and every reply answers the same top-level
 * comment, so the thread never nests further than that.
 *
 * The comment box's language is shown as a chip reading the language's name, and
 * tapping it opens a picker. The choice lasts for this visit only: it is not
 * written back to the user's profile (KNOT-ADR-048).
 */
export default function CommentThreadScreen({
  versionId,
  token,
  language,
  onOpenUserProfile,
  onBack,
}: CommentThreadScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
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
        const { version } = await versionsApi.getVersion(versionId, token);
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
  }, [hasSuggestedLanguage, token, versionId]);

  const loadFirstPage = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const page = await conversationsApi.listComments(
        versionId,
        { limit: THREAD_PAGE_SIZE },
        token,
      );
      setComments(page.comments);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [token, versionId]);

  useEffect(() => {
    void loadFirstPage();
  }, [loadFirstPage]);

  const refresh = useCallback(async (): Promise<void> => {
    setRefreshing(true);

    try {
      const page = await conversationsApi.listComments(
        versionId,
        { limit: THREAD_PAGE_SIZE },
        token,
      );
      setComments(page.comments);
      setNextCursor(page.next_cursor);
      setError(undefined);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setRefreshing(false);
    }
  }, [token, versionId]);

  const loadMore = useCallback(async (): Promise<void> => {
    if (nextCursor === '' || loadingMore) {
      return;
    }

    setLoadingMore(true);

    try {
      const page = await conversationsApi.listComments(
        versionId,
        {
          cursor: nextCursor,
          limit: THREAD_PAGE_SIZE,
        },
        token,
      );
      // Append rather than replace: the cursor is exclusive, so a page never
      // repeats a comment already on screen.
      setComments((existing) => [...existing, ...page.comments]);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoadingMore(false);
    }
  }, [loadingMore, nextCursor, token, versionId]);

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
      <Header
        title={`Comments · ${comments.length}`}
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

      {error !== undefined ? (
        <Text style={[typeScale.bodyS, styles.listError]}>{error}</Text>
      ) : null}

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
            <View style={styles.comment}>
              <View style={styles.commentHead}>
                {/* A comment row drops the Rooted badge: the thread is about the
                    discussion, not the author's rootedness. */}
                <AuthorLine
                  displayName={item.author_display_name}
                  avatarUrl={item.author_avatar_url}
                  createdAt={item.created_at}
                  rooted={null}
                  size="medium"
                  onPress={() => onOpenUserProfile(item.author_id)}
                />
                <Text style={[typeScale.caption, styles.languageCode]}>{item.language}</Text>
              </View>

              <Text style={[typeScale.body, styles.commentBody]}>{item.body}</Text>

              <View style={styles.actions}>
                <Pressable
                  style={styles.action}
                  onPress={() => handleReplyPress(item)}
                  accessibilityRole="button"
                >
                  <Text style={[typeScale.button, styles.actionText]}>Reply</Text>
                </Pressable>
                {linkLabel !== null ? (
                  <Pressable
                    style={styles.action}
                    onPress={() => handleRepliesLinkPress(item.id, replies.length)}
                    accessibilityRole="button"
                  >
                    <Text style={[typeScale.button, styles.linkText]}>{linkLabel}</Text>
                  </Pressable>
                ) : null}
              </View>

              {visibleReplies.map((reply) => (
                <View key={reply.id} style={styles.reply}>
                  <AuthorLine
                    displayName={reply.author_display_name}
                    avatarUrl={reply.author_avatar_url}
                    createdAt={reply.created_at}
                    rooted={null}
                    size="small"
                    onPress={() => onOpenUserProfile(reply.author_id)}
                  />
                  <Text style={[typeScale.body, styles.replyBody]}>{reply.body}</Text>
                  <View style={styles.actions}>
                    <Pressable
                      style={styles.action}
                      onPress={() => handleReplyPress(reply)}
                      accessibilityRole="button"
                    >
                      <Text style={[typeScale.button, styles.actionText]}>Reply</Text>
                    </Pressable>
                  </View>
                </View>
              ))}

              <View style={styles.separator} />
            </View>
          );
        }}
        ListEmptyComponent={
          !loading && error === undefined ? (
            <EmptyState
              illustrationName="no-results"
              headline="No comments yet."
              body="Start the conversation."
            />
          ) : null
        }
        ListFooterComponent={
          nextCursor !== '' ? (
            <View style={styles.loadMore}>
              <Button
                label="Load more"
                variant="secondary"
                loading={loadingMore}
                onPress={() => void loadMore()}
                fullWidth
              />
            </View>
          ) : null
        }
      />

      <View style={styles.composer}>
        {replyTo !== undefined ? (
          <View style={styles.replyBanner}>
            <Text style={[typeScale.caption, styles.replyBannerText]} numberOfLines={1}>
              Replying to {replyToName === '' ? 'this commenter' : replyToName}
            </Text>
            <Pressable
              style={styles.replyBannerCancel}
              onPress={() => setReplyTo(undefined)}
              accessibilityLabel="Cancel the reply"
              accessibilityRole="button"
            >
              <Text style={[typeScale.body, styles.replyBannerCancelText]}>✕</Text>
            </Pressable>
          </View>
        ) : null}

        <TextInput
          ref={composerRef}
          style={[typeScale.body, styles.composerInput]}
          value={body}
          onChangeText={setBody}
          placeholder={replyTo === undefined ? 'Add a comment' : 'Write a reply'}
          placeholderTextColor={colors.text.disabled}
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
            <Text style={[typeScale.caption, styles.languageChipText]} numberOfLines={1}>
              {chipLabel === '' ? 'Choose a language' : chipLabel}
            </Text>
            <Text style={[typeScale.caption, styles.languageChipChevron]}>▾</Text>
          </Pressable>
          <View style={styles.postAction}>
            <Button
              label={replyTo === undefined ? 'Post' : 'Reply'}
              loading={posting}
              onPress={() => void handlePost()}
              fullWidth
            />
          </View>
        </View>

        {composeError !== undefined ? (
          <Text style={[typeScale.bodyS, styles.error]}>{composeError}</Text>
        ) : null}
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
              <Text style={[typeScale.h3, styles.modalTitle]}>Comment language</Text>
              <Pressable
                style={styles.modalDone}
                onPress={() => setPickerOpen(false)}
                accessibilityRole="button"
                accessibilityLabel="Close the language picker"
              >
                <Text style={[typeScale.button, styles.linkText]}>Done</Text>
              </Pressable>
            </View>
            <Text style={[typeScale.bodyS, styles.hint]}>
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

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    action: {
      marginRight: spacing.lg,
    },
    actionText: {
      color: colors.text.secondary,
    },
    actions: {
      alignItems: 'center',
      flexDirection: 'row',
      flexWrap: 'wrap',
      marginTop: spacing.sm,
    },
    // A comment is a flat block on the canvas with a hairline between entries, the
    // same reading as the feed — no card (KNOT-020d).
    comment: {
      paddingHorizontal: layout.screenPaddingReading,
      paddingTop: spacing.lg,
    },
    commentBody: {
      color: colors.text.secondary,
      marginTop: spacing.sm,
    },
    commentHead: {
      alignItems: 'center',
      flexDirection: 'row',
      flexWrap: 'wrap',
      gap: spacing.sm,
    },
    composer: {
      borderTopColor: colors.border.hairline,
      borderTopWidth: 1,
      paddingBottom: spacing.xl,
      paddingHorizontal: layout.screenPaddingReading,
      paddingTop: spacing.md,
    },
    composerInput: {
      borderColor: colors.border.hairline,
      borderRadius: radius.lg,
      borderWidth: 1,
      color: colors.text.primary,
      minHeight: 44,
      paddingHorizontal: spacing.md,
      paddingVertical: spacing.sm,
    },
    composerRow: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.md,
      marginTop: spacing.md,
    },
    container: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    error: {
      color: colors.state.danger,
      marginTop: spacing.sm,
    },
    // The modal's supporting line, under its title.
    hint: {
      color: colors.text.tertiary,
      marginTop: spacing.sm,
    },
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    // The comment's ISO 639-3 code, shown beside the author.
    languageCode: {
      color: colors.text.tertiary,
    },
    languageChip: {
      alignItems: 'center',
      backgroundColor: colors.brand.tint,
      borderRadius: radius.md,
      flexDirection: 'row',
      gap: spacing.xs,
      paddingHorizontal: spacing.sm,
      paddingVertical: spacing.sm,
    },
    languageChipChevron: {
      color: colors.brand.primary,
    },
    languageChipText: {
      color: colors.brand.primary,
      maxWidth: 140,
    },
    linkText: {
      color: colors.brand.primary,
    },
    list: {
      paddingBottom: spacing.xl,
    },
    listError: {
      color: colors.state.danger,
      paddingHorizontal: layout.screenPaddingReading,
      paddingTop: spacing.md,
    },
    loadMore: {
      marginTop: spacing.lg,
      paddingHorizontal: layout.screenPaddingReading,
    },
    modalBackdrop: {
      backgroundColor: colors.scrim.overlay,
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
      backgroundColor: colors.bg.canvas,
      borderTopLeftRadius: radius.xxl,
      borderTopRightRadius: radius.xxl,
      maxHeight: '80%',
      padding: spacing.xl,
    },
    modalTitle: {
      color: colors.text.primary,
    },
    // The Post action takes the rest of the composer row.
    postAction: {
      flex: 1,
    },
    // A reply hangs off its parent with a hairline spine, indented so the thread
    // reads as one unit (KNOT-ADR-049).
    reply: {
      borderLeftColor: colors.brand.tint,
      borderLeftWidth: 1.5,
      marginLeft: spacing.sm,
      marginTop: spacing.md,
      paddingLeft: spacing.lg,
    },
    replyBanner: {
      alignItems: 'center',
      flexDirection: 'row',
      justifyContent: 'space-between',
      marginBottom: spacing.sm,
    },
    replyBannerCancel: {
      paddingHorizontal: spacing.sm,
      paddingVertical: spacing.xs,
    },
    replyBannerCancelText: {
      color: colors.text.tertiary,
    },
    replyBannerText: {
      color: colors.text.tertiary,
      flexShrink: 1,
    },
    replyBody: {
      color: colors.text.secondary,
      marginTop: spacing.xs,
    },
    // The hairline that separates one comment from the next.
    separator: {
      backgroundColor: colors.border.hairline,
      height: 1,
      marginTop: spacing.lg,
    },
    spinner: {
      marginTop: spacing.xl,
    },
  });

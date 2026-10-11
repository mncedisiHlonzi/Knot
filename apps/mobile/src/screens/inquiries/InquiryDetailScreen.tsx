import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import { describeError } from '../../api/client';
import { ANSWER_PAGE_SIZE, Inquiry, InquiryAnswer, inquiriesApi } from '../../api/inquiries';
import { EMPTY_REACTION_COUNTS, ReactionCounts } from '../../api/reactions';
import { DEFAULT_LANGUAGE_CODE } from '../../config/language';
import AuthorLine from '../../components/AuthorLine';
import ReactionBar from '../../components/ReactionBar';
import ReportAction from '../../components/ReportAction';
import { isLanguageCode, languageName } from '../../data/languages';
import { colors, fontSizes, fontWeights, lineHeights, radius, spacing } from '../../theme';
import { answerCountLabel, answerDraftError, inquiryPlaceLabel } from '../../utils/inquiry';
import { formatRelativeTime } from '../../utils/time';

type InquiryDetailScreenProps = {
  /** The inquiry to show. */
  readonly inquiryId: string;
  /** The signed-in user's access token. */
  readonly token: string;
  /** The signed-in user's preferred languages, most preferred first. */
  readonly preferredLanguages: readonly string[];
  /** Called when the person leaves this question. */
  readonly onBack: () => void;
  /** Called when a participant is tapped. */
  readonly onOpenUserProfile: (userId: string) => void;
};

/**
 * One question and its answers.
 *
 * The answers read oldest first, so the thread is a conversation: the earliest
 * answer leads and the newest is at the bottom beside the composer. That is the
 * opposite of the inquiry list, and the server guarantees it.
 *
 * The question carries reactions (KNOT-ADR-057): "adds something new" and "needs a
 * source" are ways to weigh in on a question without answering it. The answers do
 * not — an answer is a reply, and replies carry no reactions (KNOT-ADR-052).
 *
 * There is no accepted answer and no closing, because a place's knowledge is
 * plural (KNOT-ADR-055). Nothing here can mark an answer as the answer.
 */
export default function InquiryDetailScreen({
  inquiryId,
  token,
  preferredLanguages,
  onBack,
  onOpenUserProfile,
}: InquiryDetailScreenProps): React.ReactElement {
  const [inquiry, setInquiry] = useState<Inquiry | undefined>(undefined);
  const [answers, setAnswers] = useState<readonly InquiryAnswer[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const [draft, setDraft] = useState('');
  const [posting, setPosting] = useState(false);
  const [answerError, setAnswerError] = useState<string | undefined>(undefined);

  // Only a canonical ISO 639-3 code is usable: a legacy or malformed preference is
  // ignored rather than forwarded, or the server rejects the answer (KNOT-016-fix).
  const language =
    preferredLanguages.map((tag) => tag.trim()).find((tag) => isLanguageCode(tag)) ??
    DEFAULT_LANGUAGE_CODE;

  const loadFirstPage = useCallback(
    async (mode: 'initial' | 'refresh'): Promise<void> => {
      if (mode === 'initial') {
        setLoading(true);
      } else {
        setRefreshing(true);
      }
      setError(undefined);

      try {
        const [inquiryPage, answerPage] = await Promise.all([
          inquiriesApi.getInquiry(inquiryId, token),
          inquiriesApi.listAnswers(inquiryId, { limit: ANSWER_PAGE_SIZE }, token),
        ]);
        setInquiry(inquiryPage.inquiry);
        setAnswers(answerPage.answers);
        setNextCursor(answerPage.next_cursor);
      } catch (caught) {
        setError(describeError(caught));
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [inquiryId, token],
  );

  useEffect(() => {
    void loadFirstPage('initial');
  }, [loadFirstPage]);

  const loadMore = useCallback(async (): Promise<void> => {
    if (nextCursor === '' || loadingMore) {
      return;
    }

    setLoadingMore(true);
    setError(undefined);

    try {
      const page = await inquiriesApi.listAnswers(
        inquiryId,
        { cursor: nextCursor, limit: ANSWER_PAGE_SIZE },
        token,
      );
      setAnswers((existing) => [...existing, ...page.answers]);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoadingMore(false);
    }
  }, [inquiryId, loadingMore, nextCursor, token]);

  const submitAnswer = useCallback(async (): Promise<void> => {
    const validation = answerDraftError(draft);
    if (validation !== undefined) {
      setAnswerError(validation);
      return;
    }

    setPosting(true);
    setAnswerError(undefined);

    try {
      const created = await inquiriesApi.createAnswer(token, inquiryId, {
        body: draft.trim(),
        language,
      });
      // Append to the end: the thread is oldest-first, and the just-created answer
      // is the newest one.
      setAnswers((existing) => [...existing, created.answer]);
      setDraft('');
      // The answer count is maintained by the server as part of the same
      // transaction, so it moves by exactly one here rather than being refetched.
      setInquiry((existing) =>
        existing === undefined
          ? existing
          : { ...existing, answer_count: existing.answer_count + 1 },
      );
    } catch (caught) {
      setAnswerError(describeError(caught));
    } finally {
      setPosting(false);
    }
  }, [draft, inquiryId, language, token]);

  const applyReactionCounts = useCallback((counts: ReactionCounts): void => {
    setInquiry((existing) =>
      existing === undefined ? existing : { ...existing, reactions: counts },
    );
  }, []);

  return (
    <KeyboardAvoidingView
      style={styles.container}
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
    >
      <FlatList
        data={answers}
        keyExtractor={(answer) => answer.id}
        contentContainerStyle={styles.content}
        keyboardShouldPersistTaps="handled"
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={() => {
              void loadFirstPage('refresh');
            }}
          />
        }
        ListHeaderComponent={
          <View>
            <Pressable style={styles.link} onPress={onBack}>
              <Text style={styles.linkText}>← Back</Text>
            </Pressable>

            {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}
            {loading ? <ActivityIndicator style={styles.spinner} /> : null}

            {inquiry !== undefined ? (
              <View style={styles.questionCard}>
                <Text style={styles.questionTitle}>{inquiry.title}</Text>
                <Text style={styles.questionBody}>{inquiry.body}</Text>

                <View style={styles.metaRow}>
                  <Text style={styles.meta}>{inquiryPlaceLabel(inquiry.place)}</Text>
                  <Text style={styles.meta}>{answerCountLabel(inquiry.answer_count)}</Text>
                </View>

                <AuthorLine
                  displayName={inquiry.author_display_name}
                  avatarUrl={inquiry.author_avatar_url}
                  createdAt={inquiry.created_at}
                  rooted={inquiry.author_rooted}
                  size="medium"
                  onPress={() => onOpenUserProfile(inquiry.author_id)}
                />

                <ReactionBar
                  entityType="inquiry"
                  entityId={inquiry.id}
                  counts={inquiry.reactions ?? EMPTY_REACTION_COUNTS}
                  myReactions={inquiry.my_reactions ?? []}
                  token={token}
                  onChange={applyReactionCounts}
                />

                <View style={styles.reportRow}>
                  <ReportAction token={token} entityType="inquiry" entityId={inquiry.id} />
                </View>
              </View>
            ) : null}

            <Text style={styles.sectionLabel}>
              {answers.length === 0 ? 'No answers yet' : 'Answers'}
            </Text>
          </View>
        }
        renderItem={({ item }) => (
          <View style={styles.answerCard}>
            <AuthorLine
              displayName={item.author_display_name}
              avatarUrl={item.author_avatar_url}
              createdAt={item.created_at}
              rooted={item.author_rooted}
              onPress={() => onOpenUserProfile(item.author_id)}
            />
            <Text style={styles.answerBody}>{item.body}</Text>
            <Text style={styles.answerMeta}>
              {languageName(item.language)} · {formatRelativeTime(item.created_at)}
            </Text>
            <View style={styles.answerActions}>
              <ReportAction token={token} entityType="inquiry_answer" entityId={item.id} />
            </View>
          </View>
        )}
        ListFooterComponent={
          <View>
            {nextCursor !== '' ? (
              <Pressable
                style={styles.more}
                onPress={() => {
                  void loadMore();
                }}
                disabled={loadingMore}
              >
                {loadingMore ? (
                  <ActivityIndicator />
                ) : (
                  <Text style={styles.linkText}>Load more answers</Text>
                )}
              </Pressable>
            ) : null}

            <Text style={styles.sectionLabel}>Your answer</Text>
            <Text style={styles.hint}>Public and attributed, in {languageName(language)}.</Text>
            <TextInput
              style={styles.composer}
              value={draft}
              onChangeText={setDraft}
              multiline
              placeholder="Answer in your own words"
              placeholderTextColor={colors.text.secondary}
            />
            {answerError !== undefined ? <Text style={styles.error}>{answerError}</Text> : null}
            <Pressable
              style={[styles.submit, posting ? styles.submitDisabled : null]}
              onPress={() => {
                void submitAnswer();
              }}
              disabled={posting}
              accessibilityRole="button"
              accessibilityLabel="Post your answer"
            >
              {posting ? (
                <ActivityIndicator color={colors.text.primary} />
              ) : (
                <Text style={styles.submitText}>Post answer</Text>
              )}
            </Pressable>
          </View>
        }
      />
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  answerActions: {
    alignItems: 'flex-end',
  },
  reportRow: {
    alignItems: 'flex-end',
    marginTop: spacing.sm,
  },
  answerBody: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    lineHeight: lineHeights.md,
    marginBottom: spacing.sm,
    marginTop: spacing.sm,
  },
  answerCard: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.subtle,
    borderRadius: radius.lg,
    borderWidth: 1,
    marginBottom: spacing.md,
    padding: spacing.lg,
  },
  answerMeta: {
    color: colors.text.secondary,
    fontSize: fontSizes.xs,
  },
  composer: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    color: colors.text.primary,
    fontSize: fontSizes.md,
    minHeight: 96,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    textAlignVertical: 'top',
  },
  container: {
    backgroundColor: colors.bg.primary,
    flex: 1,
  },
  content: {
    padding: spacing.lg,
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.sm,
    marginBottom: spacing.md,
    marginTop: spacing.sm,
  },
  hint: {
    color: colors.text.secondary,
    fontSize: fontSizes.xs,
    marginBottom: spacing.sm,
  },
  link: {
    marginBottom: spacing.md,
  },
  linkText: {
    color: colors.text.brand,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.medium,
  },
  meta: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
  },
  metaRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: spacing.md,
    marginTop: spacing.sm,
  },
  more: {
    alignItems: 'center',
    paddingVertical: spacing.lg,
  },
  questionBody: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    lineHeight: lineHeights.md,
  },
  questionCard: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.subtle,
    borderRadius: radius.lg,
    borderWidth: 1,
    padding: spacing.lg,
  },
  questionTitle: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
    lineHeight: lineHeights.xl,
    marginBottom: spacing.sm,
  },
  sectionLabel: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.semiBold,
    marginBottom: spacing.sm,
    marginTop: spacing.lg,
    textTransform: 'uppercase',
  },
  spinner: {
    marginVertical: spacing.lg,
  },
  submit: {
    alignItems: 'center',
    backgroundColor: colors.brand.purple,
    borderRadius: radius.md,
    marginTop: spacing.md,
    paddingVertical: spacing.md,
  },
  submitDisabled: {
    opacity: 0.6,
  },
  submitText: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    fontWeight: fontWeights.semiBold,
  },
});

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
  View,
} from 'react-native';
import { ArrowLeft } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { ANSWER_PAGE_SIZE, Inquiry, InquiryAnswer, inquiriesApi } from '../../api/inquiries';
import { EMPTY_REACTION_COUNTS, ReactionCounts } from '../../api/reactions';
import { DEFAULT_LANGUAGE_CODE } from '../../config/language';
import AuthorLine from '../../components/AuthorLine';
import Button from '../../components/Button';
import Chip from '../../components/Chip';
import Header from '../../components/Header';
import Input from '../../components/Input';
import ReactionBar from '../../components/ReactionBar';
import ReportAction from '../../components/ReportAction';
import ReportModal from '../../components/ReportModal';
import { isLanguageCode, languageName } from '../../data/languages';
import { layout, spacing, typeScale, useTheme, useThemedStyles } from '../../theme';
import type { ThemeColors } from '../../theme';
import { answerCountLabel, answerDraftError } from '../../utils/inquiry';

/**
 * "INQUIRY · OAXACA, MEXICO" — the overline naming what the question is and where.
 * A question that names no place reads simply "Inquiry".
 */
function inquiryOverline(inquiry: Inquiry): string {
  const place = typeof inquiry.place === 'string' ? inquiry.place.trim() : '';
  const country = typeof inquiry.place_country === 'string' ? inquiry.place_country.trim() : '';
  const where = [place, country].filter((part) => part !== '').join(', ');
  return where === '' ? 'Inquiry' : `Inquiry · ${where}`;
}

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
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
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
  const [reportVisible, setReportVisible] = useState(false);

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
      <Header
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
        right={
          <Pressable
            style={styles.reportButton}
            onPress={() => setReportVisible(true)}
            accessibilityRole="button"
            accessibilityLabel="Report this question"
          >
            <Text style={[typeScale.label, styles.reportLabel]}>Report</Text>
          </Pressable>
        }
      />

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
            {error !== undefined ? (
              <Text style={[typeScale.bodyS, styles.error]}>{error}</Text>
            ) : null}
            {loading ? <ActivityIndicator style={styles.spinner} /> : null}

            {inquiry !== undefined ? (
              <View style={styles.question}>
                <Text style={[typeScale.overline, styles.questionOverline]}>
                  {inquiryOverline(inquiry)}
                </Text>

                <Text style={[typeScale.h1, styles.questionTitle]}>{inquiry.title}</Text>

                <View style={styles.questionAuthor}>
                  <AuthorLine
                    displayName={inquiry.author_display_name}
                    avatarUrl={inquiry.author_avatar_url}
                    createdAt={inquiry.created_at}
                    rooted={null}
                    size="medium"
                    onPress={() => onOpenUserProfile(inquiry.author_id)}
                  />
                </View>

                <Text style={[typeScale.reading, styles.questionBody]}>{inquiry.body}</Text>

                <ReactionBar
                  entityType="inquiry"
                  entityId={inquiry.id}
                  counts={inquiry.reactions ?? EMPTY_REACTION_COUNTS}
                  myReactions={inquiry.my_reactions ?? []}
                  token={token}
                  onChange={applyReactionCounts}
                />
              </View>
            ) : null}

            <View style={styles.answersBar}>
              <Text style={[typeScale.label, styles.answersBarLabel]}>
                {answerCountLabel(inquiry?.answer_count ?? answers.length)}
              </Text>
            </View>
          </View>
        }
        renderItem={({ item }) => (
          <View style={styles.answer}>
            <View style={styles.answerHead}>
              <View style={styles.answerIdentity}>
                <AuthorLine
                  displayName={item.author_display_name}
                  avatarUrl={item.author_avatar_url}
                  createdAt={item.created_at}
                  rooted={null}
                  onPress={() => onOpenUserProfile(item.author_id)}
                />
              </View>
              <Text style={[typeScale.caption, styles.answerLanguage]} numberOfLines={1}>
                {languageName(item.language)}
              </Text>
              <ReportAction token={token} entityType="inquiry_answer" entityId={item.id} />
            </View>
            <Text style={[typeScale.body, styles.answerBody]}>{item.body}</Text>
          </View>
        )}
        ListFooterComponent={
          <View>
            {nextCursor !== '' ? (
              <View style={styles.more}>
                <Button
                  variant="ghost"
                  label="Load more answers"
                  loading={loadingMore}
                  onPress={() => {
                    void loadMore();
                  }}
                />
              </View>
            ) : null}

            <Text style={[typeScale.overline, styles.composerLabel]}>Your answer</Text>
            <Text style={[typeScale.caption, styles.composerHint]}>
              Public and attributed, in {languageName(language)}.
            </Text>
            <View style={styles.composerLang}>
              <Chip label={languageName(language)} selected />
            </View>
            <Input
              value={draft}
              onChangeText={setDraft}
              multiline
              accessibilityLabel="Write an answer"
              placeholder="Answer in your own words"
            />
            {answerError !== undefined ? (
              <Text style={[typeScale.bodyS, styles.error]}>{answerError}</Text>
            ) : null}
            <View style={styles.composerSubmit}>
              <Button
                variant="primary"
                fullWidth
                label="Post answer"
                loading={posting}
                onPress={() => {
                  void submitAnswer();
                }}
              />
            </View>
          </View>
        }
      />

      {inquiry !== undefined ? (
        <ReportModal
          visible={reportVisible}
          token={token}
          entityType="inquiry"
          entityId={inquiry.id}
          onClose={() => setReportVisible(false)}
          onSubmitted={() => setReportVisible(false)}
        />
      ) : null}
    </KeyboardAvoidingView>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    answer: {
      borderBottomColor: colors.border.hairline,
      borderBottomWidth: 1,
      marginHorizontal: -layout.screenPaddingReading,
      paddingHorizontal: layout.screenPaddingReading,
      paddingVertical: spacing.lg,
    },
    answerBody: {
      color: colors.text.secondary,
      marginTop: spacing.sm,
    },
    answerHead: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.sm,
    },
    answerIdentity: {
      flexShrink: 1,
    },
    answerLanguage: {
      color: colors.text.tertiary,
    },
    // The count bar between the question and its answers: hairlines above and
    // below, full-bleed across the reading column.
    answersBar: {
      borderBottomColor: colors.border.hairline,
      borderBottomWidth: 1,
      borderTopColor: colors.border.hairline,
      borderTopWidth: 1,
      marginHorizontal: -layout.screenPaddingReading,
      marginTop: spacing.lg,
      paddingHorizontal: layout.screenPaddingReading,
      paddingVertical: spacing.md,
    },
    answersBarLabel: {
      color: colors.text.primary,
    },
    composerHint: {
      color: colors.text.tertiary,
      marginTop: spacing.xs,
    },
    composerLabel: {
      color: colors.text.secondary,
      marginTop: spacing.xl,
    },
    composerLang: {
      marginBottom: spacing.md,
      marginTop: spacing.md,
    },
    composerSubmit: {
      marginTop: spacing.md,
    },
    container: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    content: {
      paddingBottom: spacing.xxxl,
      paddingHorizontal: layout.screenPaddingReading,
      paddingTop: spacing.xs,
    },
    error: {
      color: colors.state.danger,
      marginTop: spacing.sm,
    },
    // A 44-point tap target for each header icon button.
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    more: {
      paddingVertical: spacing.lg,
    },
    question: {
      paddingTop: spacing.xs,
    },
    questionAuthor: {
      marginTop: spacing.md,
    },
    questionBody: {
      color: colors.text.secondary,
      marginTop: spacing.md,
    },
    questionOverline: {
      color: colors.brand.primary,
    },
    questionTitle: {
      color: colors.text.primary,
      marginTop: spacing.xs,
    },
    // The header's report action: a text button in the danger ink, which is how
    // the app marks a destructive action everywhere else (KNOT-020j-fix2).
    reportButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      paddingHorizontal: spacing.sm,
    },
    reportLabel: {
      color: colors.state.danger,
    },
    spinner: {
      marginVertical: spacing.lg,
    },
  });

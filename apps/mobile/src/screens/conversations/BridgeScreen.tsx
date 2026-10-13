import React, { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { ArrowLeft } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { Comment, CreateBridgePayload, conversationsApi } from '../../api/conversations';
import AuthorLine from '../../components/AuthorLine';
import Button from '../../components/Button';
import Header from '../../components/Header';
import Input from '../../components/Input';
import LanguagePicker from '../../components/LanguagePicker';
import { isLanguageCode, languageName } from '../../data/languages';
import { colors, layout, spacing, typeScale } from '../../theme';

type BridgeScreenProps = {
  /**
   * The comment being bridged. It carries the source comment id the server
   * needs, and gives this screen the context to show.
   */
  readonly sourceComment: Comment;
  /** The signed-in user's access token. The server derives the bridger from it. */
  readonly token: string;
  /**
   * The user's preferred language tags, used to guess the target language: the
   * first one that differs from the source comment's language.
   */
  readonly preferredLanguages: readonly string[];
  /** Called once the bridge has been created. */
  readonly onBridged: () => void;
  /** Called when the source comment's author is tapped, to open their profile. */
  readonly onOpenUserProfile: (userId: string) => void;
  /** Called when the person abandons the form. */
  readonly onCancel: () => void;
};

/** Field limits, mirroring the server's rules so the user is told early. */
const MAX_BODY_LENGTH = 5000;
const MAX_NOTE_LENGTH = 1000;

/**
 * The first preferred language that differs from the source comment's language,
 * or "" when none does.
 *
 * A preference is only used when it is a canonical code, so an older or
 * unexpected tag cannot prefill the form with something the server would reject
 * (KNOT-ADR-045).
 */
function defaultTargetLanguage(sourceLanguage: string, preferred: readonly string[]): string {
  const candidate = preferred
    .map((tag) => tag.trim())
    .find((tag) => isLanguageCode(tag) && tag !== sourceLanguage);

  return candidate ?? '';
}

/**
 * Returns the first client-side validation problem, or undefined when the form is
 * acceptable. The server validates again and remains the source of truth.
 */
function validateForm(
  body: string,
  targetLanguage: string,
  sourceLanguage: string,
  note: string,
): string | undefined {
  if (!isLanguageCode(targetLanguage)) {
    return 'Choose the language you are bridging into.';
  }
  if (targetLanguage === sourceLanguage) {
    return 'The target language must differ from the comment you are bridging.';
  }
  if (body.trim() === '') {
    return 'A comment is required.';
  }
  if (body.length > MAX_BODY_LENGTH) {
    return `The comment must be at most ${MAX_BODY_LENGTH} characters.`;
  }
  if (note.trim().length > MAX_NOTE_LENGTH) {
    return `The note must be at most ${MAX_NOTE_LENGTH} characters.`;
  }
  return undefined;
}

/**
 * Bridge a comment into another language.
 *
 * A bridge writes a new comment in the target language and joins it to the one
 * it came from. Both comments stay where they are: the source conversation is
 * untouched, and the new comment joins it in the target language (KNOT-ADR-014).
 */
export default function BridgeScreen({
  sourceComment,
  token,
  preferredLanguages,
  onBridged,
  onOpenUserProfile,
  onCancel,
}: BridgeScreenProps): React.ReactElement {
  const [targetLanguage, setTargetLanguage] = useState(() =>
    defaultTargetLanguage(sourceComment.language, preferredLanguages),
  );
  const [body, setBody] = useState('');
  const [note, setNote] = useState('');
  const [error, setError] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(): Promise<void> {
    const problem = validateForm(body, targetLanguage, sourceComment.language, note);
    if (problem !== undefined) {
      setError(problem);
      return;
    }

    setError(undefined);
    setSubmitting(true);

    const payload: CreateBridgePayload = {
      target_language: targetLanguage,
      body,
      ...(note.trim() === '' ? {} : { adaptation_note: note.trim() }),
    };

    try {
      await conversationsApi.createBridge(sourceComment.id, token, payload);
      onBridged();
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <View style={styles.container}>
      <Header
        title="Bridge this comment"
        left={
          <Pressable
            style={styles.iconButton}
            onPress={onCancel}
            accessibilityRole="button"
            accessibilityLabel="Close"
          >
            <ArrowLeft color={colors.text.primary} size={24} />
          </Pressable>
        }
      />

      <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        {/* The comment being bridged, quiet and hairline-framed. */}
        <View style={styles.sourceBlock}>
          <AuthorLine
            displayName={sourceComment.author_display_name}
            avatarUrl={sourceComment.author_avatar_url}
            createdAt={sourceComment.created_at}
            rooted={sourceComment.author_rooted}
            size="large"
            onPress={() => onOpenUserProfile(sourceComment.author_id)}
          />
          <Text style={[typeScale.caption, styles.sourceLanguage]}>
            {languageName(sourceComment.language)} · {sourceComment.language}
          </Text>
          <Text style={[typeScale.body, styles.sourceBody]}>{sourceComment.body}</Text>
        </View>

        <View>
          <Text style={[typeScale.label, styles.fieldLabel]}>Bridge into</Text>
          <LanguagePicker
            mode="single"
            selected={targetLanguage}
            onSelect={(chosen) => setTargetLanguage(chosen.code)}
            onClear={() => setTargetLanguage('')}
            placeholder="Search, e.g. French or fra"
          />

          <View style={styles.field}>
            <Input
              label="Write it the way you would say it"
              value={body}
              onChangeText={setBody}
              placeholder="Say it in the other language"
              helper="Stay close to the meaning. A note can explain a word with no match."
              multiline
            />
          </View>

          <View style={styles.field}>
            <Input
              label="Note (optional)"
              value={note}
              onChangeText={setNote}
              placeholder="Add a note"
              helper="Explain a choice you made, so readers can trust it."
              multiline
            />
          </View>

          {error !== undefined ? (
            <Text style={[typeScale.bodyS, styles.error]}>{error}</Text>
          ) : null}

          <View style={styles.submit}>
            <Button
              label="Bridge comment"
              onPress={() => void handleSubmit()}
              loading={submitting}
              fullWidth
            />
          </View>
        </View>
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    backgroundColor: colors.bg.canvas,
    flex: 1,
  },
  content: {
    paddingBottom: spacing.xxxl,
    paddingHorizontal: layout.screenPaddingReading,
  },
  error: {
    color: colors.state.danger,
    marginTop: spacing.lg,
  },
  field: {
    marginTop: spacing.lg,
  },
  fieldLabel: {
    color: colors.text.tertiary,
    marginBottom: spacing.sm,
    marginTop: spacing.xl,
  },
  iconButton: {
    alignItems: 'center',
    height: 44,
    justifyContent: 'center',
    width: 44,
  },
  // The source comment is framed by a hairline on its left, per the reference.
  sourceBlock: {
    borderLeftColor: colors.brand.tint,
    borderLeftWidth: 2,
    marginTop: spacing.lg,
    paddingLeft: spacing.md,
  },
  sourceBody: {
    color: colors.text.secondary,
    marginTop: spacing.sm,
  },
  sourceLanguage: {
    color: colors.text.tertiary,
    marginTop: spacing.xs,
  },
  submit: {
    marginTop: spacing.xl,
  },
});

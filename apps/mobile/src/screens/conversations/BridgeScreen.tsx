import React, { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, TextInput } from 'react-native';

import { describeError } from '../../api/client';
import { Comment, CreateBridgePayload, conversationsApi } from '../../api/conversations';
import { colors, fontSizes, fontWeights, lineHeights, radius, spacing } from '../../theme';

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
  /** Called when the person abandons the form. */
  readonly onCancel: () => void;
};

/** Field limits, mirroring the server's rules so the user is told early. */
const MAX_BODY_LENGTH = 5000;
const MAX_NOTE_LENGTH = 1000;
const LANGUAGE_PATTERN = /^[A-Za-z]{2,8}$/;

/** The first eight characters of an author id, so contributors are distinguishable. */
function authorPrefix(authorId: string): string {
  return authorId.slice(0, 8);
}

/**
 * The first preferred language that differs from the source comment's language,
 * or "" when none does.
 */
function defaultTargetLanguage(sourceLanguage: string, preferred: readonly string[]): string {
  const candidate = preferred.find((tag) => {
    const trimmed = tag.trim().toLowerCase();
    return trimmed !== '' && trimmed !== sourceLanguage;
  });
  return candidate === undefined ? '' : candidate.trim().toLowerCase();
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
  if (!LANGUAGE_PATTERN.test(targetLanguage.trim())) {
    return 'The target language must be 2 to 8 letters, such as fr or zu.';
  }
  if (targetLanguage.trim().toLowerCase() === sourceLanguage) {
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
      target_language: targetLanguage.trim().toLowerCase(),
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
    <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
      <Pressable style={styles.link} onPress={onCancel}>
        <Text style={styles.linkText}>← Back</Text>
      </Pressable>

      <Text style={styles.title}>Bridge to another language</Text>
      <Text style={styles.hint}>
        Your comment is added to the conversation in the language you choose, joined to the one you
        are bridging. The story must already have a version in that language, and the original
        comment stays as it is.
      </Text>

      <Text style={styles.label}>You are bridging</Text>
      <Text style={styles.sourceMeta}>
        {sourceComment.language} · {authorPrefix(sourceComment.author_id)}
      </Text>
      <Text style={styles.sourceBody}>{sourceComment.body}</Text>

      <Text style={styles.label}>Target language</Text>
      <TextInput
        style={styles.input}
        value={targetLanguage}
        onChangeText={setTargetLanguage}
        autoCapitalize="none"
        autoCorrect={false}
        placeholder="fr"
        placeholderTextColor={colors.text.secondary}
      />

      <Text style={styles.label}>Your comment</Text>
      <TextInput
        style={[styles.input, styles.multiline]}
        value={body}
        onChangeText={setBody}
        placeholder="Say it in the other language"
        placeholderTextColor={colors.text.secondary}
        multiline
        numberOfLines={6}
        textAlignVertical="top"
      />

      <Text style={styles.label}>Note for readers (optional)</Text>
      <TextInput
        style={[styles.input, styles.multiline]}
        value={note}
        onChangeText={setNote}
        placeholder="What changed, and why?"
        placeholderTextColor={colors.text.secondary}
        multiline
        numberOfLines={3}
        textAlignVertical="top"
      />

      {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

      <Pressable
        style={[styles.button, submitting ? styles.disabled : null]}
        onPress={handleSubmit}
        disabled={submitting}
      >
        <Text style={styles.buttonText}>{submitting ? 'Adding…' : 'Add my bridge'}</Text>
      </Pressable>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  button: {
    alignItems: 'center',
    backgroundColor: colors.brand.purple,
    borderRadius: radius.md,
    marginTop: spacing.xl,
    paddingVertical: 14,
  },
  buttonText: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    fontWeight: fontWeights.semiBold,
  },
  content: {
    backgroundColor: colors.bg.primary,
    padding: spacing.xl,
    paddingBottom: spacing['3xl'],
  },
  disabled: {
    opacity: 0.5,
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.base,
    marginTop: spacing.lg,
  },
  hint: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: spacing.sm,
  },
  input: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    color: colors.text.primary,
    fontSize: fontSizes.md,
    marginTop: 6,
    paddingHorizontal: spacing.md,
    paddingVertical: 10,
  },
  label: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
    marginTop: 20,
  },
  link: {
    marginBottom: spacing.md,
  },
  linkText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
  },
  multiline: {
    minHeight: 100,
  },
  sourceBody: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    lineHeight: lineHeights.base,
    marginTop: 6,
  },
  sourceMeta: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: 6,
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
  },
});

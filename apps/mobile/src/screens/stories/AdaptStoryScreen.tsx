import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
} from 'react-native';

import { describeError } from '../../api/client';
import { CreateAdaptationPayload, StoryVersion, versionsApi } from '../../api/versions';
import LanguagePicker from '../../components/LanguagePicker';
import { isLanguageCode, languageName } from '../../data/languages';
import { colors, fontSizes, fontWeights, radius, spacing } from '../../theme';

type AdaptStoryScreenProps = {
  /** The story the new version belongs to. */
  readonly storyId: string;
  /** The version being adapted. It must belong to the same story. */
  readonly parentVersionId: string;
  /** The signed-in user's access token. The server derives the adapter from it. */
  readonly token: string;
  /** The language the form starts with, e.g. the user's first preferred tag. */
  readonly defaultLanguage: string;
  /** Called with the new version once it has been stored. */
  readonly onAdapted: (version: StoryVersion) => void;
  /** Called when the person abandons the form. */
  readonly onCancel: () => void;
};

/** Field limits, mirroring the server's rules so the user is told early. */
const MAX_TITLE_LENGTH = 200;
const MAX_NOTE_LENGTH = 1000;

/** The language the form starts with when the caller offers nothing usable. */
const DEFAULT_LANGUAGE = 'en';

/**
 * Returns the first client-side validation problem, or undefined when the form is
 * acceptable. The server validates again and remains the source of truth.
 */
function validateForm(
  title: string,
  body: string,
  language: string,
  note: string,
): string | undefined {
  if (title.trim() === '') {
    return 'A title is required.';
  }
  if (title.trim().length > MAX_TITLE_LENGTH) {
    return `The title must be at most ${MAX_TITLE_LENGTH} characters.`;
  }
  if (body.trim() === '') {
    return 'The story itself is required.';
  }
  if (!isLanguageCode(language)) {
    return 'Choose the language you are telling this version in.';
  }
  if (note.trim().length > MAX_NOTE_LENGTH) {
    return `The note must be at most ${MAX_NOTE_LENGTH} characters.`;
  }
  return undefined;
}

/**
 * Tell My People: adapt a story for the people you tell it to.
 *
 * The form starts from the version being adapted — the title and body are
 * prefilled so the adapter edits a retelling rather than retyping the story.
 * There is no author field: the adapter is the signed-in account, decided by the
 * server from the access token.
 */
export default function AdaptStoryScreen({
  storyId,
  parentVersionId,
  token,
  defaultLanguage,
  onAdapted,
  onCancel,
}: AdaptStoryScreenProps): React.ReactElement {
  const [parent, setParent] = useState<StoryVersion | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | undefined>(undefined);

  // The caller's suggestion is only used when it is a canonical code, so an
  // older or unexpected tag cannot prefill the form with something the server
  // would reject.
  const [language, setLanguage] = useState(() => {
    const suggested = defaultLanguage.trim();
    return isLanguageCode(suggested) ? suggested : DEFAULT_LANGUAGE;
  });
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [note, setNote] = useState('');
  const [error, setError] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setLoadError(undefined);

    try {
      const result = await versionsApi.getVersion(parentVersionId);
      setParent(result.version);
      // Prefill from the version being adapted so the adapter edits a retelling.
      setTitle(result.version.title);
      setBody(result.version.body);
    } catch (caught) {
      setLoadError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [parentVersionId]);

  useEffect(() => {
    void load();
  }, [load]);

  async function handleSubmit(): Promise<void> {
    const problem = validateForm(title, body, language, note);
    if (problem !== undefined) {
      setError(problem);
      return;
    }

    setError(undefined);
    setSubmitting(true);

    const payload: CreateAdaptationPayload = {
      parent_version_id: parentVersionId,
      language: language,
      title: title.trim(),
      body,
      ...(note.trim() === '' ? {} : { adaptation_note: note.trim() }),
    };

    try {
      const result = await versionsApi.createAdaptation(storyId, token, payload);
      onAdapted(result.version);
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

      <Text style={styles.title}>Adapt for my people</Text>
      <Text style={styles.hint}>
        Tell this story the way your people would tell it. Your version is added to the language
        tree beside the one it comes from.
      </Text>

      {loading ? <ActivityIndicator style={styles.spinner} /> : null}
      {loadError !== undefined ? <Text style={styles.error}>{loadError}</Text> : null}

      {!loading && parent !== undefined ? (
        <>
          <Text style={styles.parentSummary}>
            Adapting the {languageName(parent.language)} version: {parent.title}
          </Text>

          <Text style={styles.label}>Language</Text>
          <LanguagePicker
            mode="single"
            selected={language}
            onSelect={(chosen) => setLanguage(chosen.code)}
            onClear={() => setLanguage('')}
            placeholder="Search, e.g. French or fr"
          />

          <Text style={styles.label}>Title</Text>
          <TextInput
            style={styles.input}
            value={title}
            onChangeText={setTitle}
            placeholder="A short headline"
            placeholderTextColor={colors.text.secondary}
          />

          <Text style={styles.label}>Story</Text>
          <TextInput
            style={[styles.input, styles.multiline]}
            value={body}
            onChangeText={setBody}
            placeholder="Tell it the way you would tell it out loud"
            placeholderTextColor={colors.text.secondary}
            multiline
            numberOfLines={8}
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
            style={[styles.button, submitting ? styles.buttonDisabled : null]}
            onPress={handleSubmit}
            disabled={submitting}
          >
            <Text style={styles.buttonText}>{submitting ? 'Adding…' : 'Add my version'}</Text>
          </Pressable>
        </>
      ) : null}

      {!loading && parent === undefined && loadError !== undefined ? (
        <Pressable style={styles.secondaryButton} onPress={load}>
          <Text style={styles.secondaryButtonText}>Try again</Text>
        </Pressable>
      ) : null}
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
  buttonDisabled: {
    opacity: 0.5,
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
    minHeight: 120,
  },
  parentSummary: {
    color: colors.text.secondary,
    fontSize: fontSizes.base,
    marginTop: spacing.lg,
  },
  secondaryButton: {
    alignItems: 'center',
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: 20,
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

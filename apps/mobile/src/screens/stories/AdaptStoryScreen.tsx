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
const LANGUAGE_PATTERN = /^[A-Za-z]{2,8}$/;

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
  if (!LANGUAGE_PATTERN.test(language.trim())) {
    return 'The language must be 2 to 8 letters, such as fr or zu.';
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

  const [language, setLanguage] = useState(
    defaultLanguage.trim() === '' ? 'en' : defaultLanguage.trim(),
  );
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
      language: language.trim().toLowerCase(),
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
        <Text style={styles.linkText}>Cancel</Text>
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
            Adapting the {parent.language} version: {parent.title}
          </Text>

          <Text style={styles.label}>Language</Text>
          <TextInput
            style={styles.input}
            value={language}
            onChangeText={setLanguage}
            autoCapitalize="none"
            autoCorrect={false}
            placeholder="fr"
            placeholderTextColor="#9aa0a6"
          />

          <Text style={styles.label}>Title</Text>
          <TextInput
            style={styles.input}
            value={title}
            onChangeText={setTitle}
            placeholder="A short headline"
            placeholderTextColor="#9aa0a6"
          />

          <Text style={styles.label}>Story</Text>
          <TextInput
            style={[styles.input, styles.multiline]}
            value={body}
            onChangeText={setBody}
            placeholder="Tell it the way you would tell it out loud"
            placeholderTextColor="#9aa0a6"
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
            placeholderTextColor="#9aa0a6"
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
    backgroundColor: '#1f6feb',
    borderRadius: 8,
    marginTop: 24,
    paddingVertical: 14,
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  buttonText: {
    color: '#ffffff',
    fontSize: 16,
    fontWeight: '600',
  },
  content: {
    padding: 24,
    paddingBottom: 48,
  },
  error: {
    color: '#b3261e',
    fontSize: 14,
    marginTop: 16,
  },
  hint: {
    color: '#57606a',
    fontSize: 13,
    marginTop: 8,
  },
  input: {
    borderColor: '#d0d7de',
    borderRadius: 8,
    borderWidth: 1,
    color: '#24292f',
    fontSize: 16,
    marginTop: 6,
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  label: {
    color: '#24292f',
    fontSize: 15,
    fontWeight: '600',
    marginTop: 20,
  },
  link: {
    marginBottom: 12,
  },
  linkText: {
    color: '#1f6feb',
    fontSize: 15,
  },
  multiline: {
    minHeight: 120,
  },
  parentSummary: {
    color: '#57606a',
    fontSize: 14,
    marginTop: 16,
  },
  secondaryButton: {
    alignItems: 'center',
    borderColor: '#d0d7de',
    borderRadius: 8,
    borderWidth: 1,
    marginTop: 20,
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

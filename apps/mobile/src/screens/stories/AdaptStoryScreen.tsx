import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { ArrowLeft } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { CreateAdaptationPayload, StoryVersion, versionsApi } from '../../api/versions';
import { DEFAULT_LANGUAGE_CODE } from '../../config/language';
import Button from '../../components/Button';
import Header from '../../components/Header';
import Input from '../../components/Input';
import LanguagePicker from '../../components/LanguagePicker';
import { isLanguageCode, languageName } from '../../data/languages';
import { colors, layout, radius, spacing, typeScale } from '../../theme';

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
const DEFAULT_LANGUAGE = DEFAULT_LANGUAGE_CODE;

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
      const result = await versionsApi.getVersion(parentVersionId, token);
      setParent(result.version);
      // Prefill from the version being adapted so the adapter edits a retelling.
      setTitle(result.version.title);
      setBody(result.version.body);
    } catch (caught) {
      setLoadError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [parentVersionId, token]);

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
    <View style={styles.container}>
      <Header
        title="Tell My People"
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
        {loading ? <ActivityIndicator style={styles.spinner} /> : null}
        {loadError !== undefined ? (
          <Text style={[typeScale.bodyS, styles.error]}>{loadError}</Text>
        ) : null}

        {!loading && parent !== undefined ? (
          <>
            {/* The version being adapted, as the tree draws it: a marker on the
                spine, then the quiet source line. */}
            <View style={styles.spineRow}>
              <View style={styles.spineGutter}>
                <View style={[styles.marker, styles.markerFilled]} />
                <View style={styles.spine} />
              </View>
              <View style={styles.spineBody}>
                <Text style={[typeScale.title, styles.sourceTitle]} numberOfLines={2}>
                  {parent.title}
                </Text>
                <Text style={[typeScale.caption, styles.sourceMeta]}>
                  {languageName(parent.language)} · {parent.language}
                </Text>
              </View>
            </View>

            <View style={styles.spineRow}>
              <View style={styles.spineGutter}>
                <View style={[styles.marker, styles.markerHollow]} />
              </View>
              <Text style={[typeScale.label, styles.sectionHeading]}>
                Telling it to your people
              </Text>
            </View>

            <View style={styles.form}>
              <Text style={[typeScale.label, styles.fieldLabel]}>Language</Text>
              <LanguagePicker
                mode="single"
                selected={language}
                onSelect={(chosen) => setLanguage(chosen.code)}
                onClear={() => setLanguage('')}
                placeholder="Search, e.g. French or fra"
              />

              <View style={styles.field}>
                <Input
                  label="Title in your language"
                  value={title}
                  onChangeText={setTitle}
                  placeholder="A short headline"
                />
              </View>

              <View style={styles.field}>
                <Input
                  label="Tell it the way your people would"
                  value={body}
                  onChangeText={setBody}
                  placeholder="Tell it the way you would tell it out loud"
                  multiline
                />
              </View>

              <View style={styles.noteBlock}>
                <Input
                  label="Adaptation note (optional)"
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
                  label="Publish for my people"
                  onPress={() => void handleSubmit()}
                  loading={submitting}
                  fullWidth
                />
              </View>
            </View>
          </>
        ) : null}

        {!loading && parent === undefined && loadError !== undefined ? (
          <View style={styles.submit}>
            <Button label="Try again" variant="secondary" onPress={() => void load()} fullWidth />
          </View>
        ) : null}
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
  },
  // The form sits under the source's text column, matching the reference's indent.
  form: {
    marginLeft: spacing.xxl,
  },
  iconButton: {
    alignItems: 'center',
    height: 44,
    justifyContent: 'center',
    width: 44,
  },
  marker: {
    borderRadius: radius.pill,
    borderWidth: 2,
    height: 12,
    left: 0,
    position: 'absolute',
    top: 4,
    width: 12,
  },
  markerFilled: {
    backgroundColor: colors.brand.primary,
    borderColor: colors.brand.primary,
  },
  markerHollow: {
    backgroundColor: colors.bg.canvas,
    borderColor: colors.brand.primary,
  },
  noteBlock: {
    borderTopColor: colors.border.hairline,
    borderTopWidth: 1,
    marginTop: spacing.lg,
    paddingTop: spacing.lg,
  },
  sectionHeading: {
    color: colors.brand.primary,
    marginBottom: spacing.sm,
    marginTop: spacing.md,
  },
  sourceMeta: {
    color: colors.text.tertiary,
    marginTop: spacing.xs,
  },
  sourceTitle: {
    color: colors.text.primary,
  },
  // A row of the tree: a marker on a spine, then the text column.
  spineBody: {
    flexShrink: 1,
    paddingBottom: spacing.xl,
  },
  spineGutter: {
    marginRight: spacing.md,
    width: 12,
  },
  spineRow: {
    flexDirection: 'row',
    paddingTop: spacing.lg,
  },
  // The spine drops from the marker down through the block.
  spine: {
    borderLeftColor: colors.brand.tint,
    borderLeftWidth: 1.5,
    bottom: 0,
    left: 5,
    position: 'absolute',
    top: 16,
  },
  spinner: {
    marginTop: spacing.xl,
  },
  submit: {
    marginTop: spacing.xl,
  },
});

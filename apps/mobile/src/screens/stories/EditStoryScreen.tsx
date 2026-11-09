import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import { X } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { type Story, storiesApi } from '../../api/stories';
import Button from '../../components/Button';
import Header from '../../components/Header';
import { layout, spacing, typeScale, useTheme, useThemedStyles } from '../../theme';
import type { ThemeColors } from '../../theme';

/**
 * Field limits, mirroring the server's rules so the author is told before the
 * request rather than by a 400 (KNOT-054).
 */
const MAX_TITLE_LENGTH = 200;
const MAX_BODY_LENGTH = 10000;

/** The title field's prompt, the same words the composer uses. */
const TITLE_PLACEHOLDER = 'Give your story a headline';

/** The body field's prompt, the same words the composer uses. */
const BODY_PLACEHOLDER = 'Tell it the way you would tell it out loud';

/**
 * Checks an edit before it is sent. Returns the sentence to show, or undefined
 * when the draft is valid.
 *
 * The rules are the server's own: a title is required and at most 200 characters,
 * a body is required and at most 10,000. The title is measured after trimming
 * because the server trims it too, so "   " is empty on both sides.
 */
export function validateStoryEdit(title: string, body: string): string | undefined {
  if (title.trim() === '') {
    return 'A title is required.';
  }
  if (title.trim().length > MAX_TITLE_LENGTH) {
    return `The title must be at most ${MAX_TITLE_LENGTH} characters.`;
  }
  if (body.trim() === '') {
    return 'A story is required.';
  }
  if (body.length > MAX_BODY_LENGTH) {
    return `The story must be at most ${MAX_BODY_LENGTH} characters.`;
  }
  return undefined;
}

type EditStoryScreenProps = {
  /** The story to revise. */
  readonly storyId: string;
  /** The signed-in author's access token; the server refuses anyone else. */
  readonly token: string;
  /** Called with the updated story after a successful save. */
  readonly onSaved: (story: Story) => void;
  /** Called when the author leaves without saving. */
  readonly onCancel: () => void;
};

/**
 * Edit Story: a text-only revision of a story the signed-in author owns
 * (KNOT-054).
 *
 * It is deliberately narrower than the composer. Editing here changes the title
 * and the body — a story's written content — and nothing else: a language change
 * is an adaptation, and adding or removing pictures is a different feature. The
 * screen therefore has no media strip and no pickers, which is also why it is a
 * screen of its own rather than the composer reopened in a mode.
 *
 * The draft is read from the server rather than passed in, so the screen is correct
 * whatever entry point opened it: the Feed's menu, the profile wall, or the story
 * detail. It writes through `PUT /stories/{id}` and hands the updated story back so
 * the caller can replace what it is showing without a second read.
 */
export default function EditStoryScreen({
  storyId,
  token,
  onSaved,
  onCancel,
}: EditStoryScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  /** Reads the story so the fields open with what is already written. */
  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const response = await storiesApi.getStory(storyId, token);
      setTitle(response.story.title);
      setBody(response.story.body);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [storyId, token]);

  useEffect(() => {
    void load();
  }, [load]);

  const handleSave = useCallback(async (): Promise<void> => {
    const problem = validateStoryEdit(title, body);
    if (problem !== undefined) {
      setError(problem);
      return;
    }

    setSaving(true);
    setError(undefined);

    try {
      // The server trims the title; sending the title unbled keeps the local
      // preview honest, and the body is sent verbatim so formatting survives.
      const response = await storiesApi.updateStory(storyId, token, {
        title: title.trim(),
        body,
      });
      onSaved(response.story);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setSaving(false);
    }
  }, [body, onSaved, storyId, title, token]);

  return (
    <View style={styles.container}>
      <Header
        title="Edit story"
        left={
          <Pressable
            style={styles.iconButton}
            onPress={onCancel}
            accessibilityRole="button"
            accessibilityLabel="Close without saving"
            testID="edit-close"
          >
            <X color={colors.text.primary} size={22} weight="bold" />
          </Pressable>
        }
        right={
          <Button
            variant="primary"
            label="Save"
            loading={saving}
            accessibilityLabel="Save the story"
            onPress={() => {
              void handleSave();
            }}
            testID="edit-save"
          />
        }
      />

      {loading ? (
        <ActivityIndicator style={styles.spinner} />
      ) : (
        <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
          {error === undefined ? null : <Text style={styles.error}>{error}</Text>}

          {/* Borderless fields, exactly the composer's: the headline at `h1`, the
              story at `reading`, with no label and no underline. */}
          <TextInput
            style={[typeScale.h1, styles.title]}
            value={title}
            onChangeText={setTitle}
            placeholder={TITLE_PLACEHOLDER}
            placeholderTextColor={colors.text.disabled}
            accessibilityLabel="The story's title"
            multiline
            testID="edit-title"
          />
          <TextInput
            style={[typeScale.reading, styles.body]}
            value={body}
            onChangeText={setBody}
            placeholder={BODY_PLACEHOLDER}
            placeholderTextColor={colors.text.disabled}
            accessibilityLabel="The story"
            multiline
            textAlignVertical="top"
            testID="edit-body"
          />
        </ScrollView>
      )}
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    body: {
      color: colors.text.primary,
      marginTop: spacing.md,
      minHeight: 240,
      paddingVertical: 0,
    },
    container: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    content: {
      paddingBottom: spacing.xxxl,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingTop: spacing.md,
    },
    error: {
      color: colors.state.danger,
      marginBottom: spacing.md,
    },
    // A 44-point tap target for the header's close control.
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    spinner: {
      marginTop: spacing.xl,
    },
    title: {
      color: colors.text.primary,
      paddingVertical: 0,
    },
  });

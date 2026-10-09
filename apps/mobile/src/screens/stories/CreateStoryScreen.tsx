import React, { useState } from 'react';
import {
  Image,
  Pressable,
  ScrollView,
  StyleSheet,
  Switch,
  Text,
  TextInput,
  View,
} from 'react-native';

import { describeError } from '../../api/client';
import {
  CreateStoryPayload,
  PILLARS,
  Pillar,
  Story,
  storiesApi,
  uploadStoryMedia,
} from '../../api/stories';
import CameraCaptureBadge from '../../components/CameraCaptureBadge';
import MediaPickerSheet, { PickedMedia } from '../../components/MediaPickerSheet';
import { colors, fontSizes, fontWeights, radius, spacing } from '../../theme';

type CreateStoryScreenProps = {
  /** The signed-in user's access token. The server derives the author from it. */
  readonly token: string;
  /** Called with the published story once it has been stored. */
  readonly onCreated: (story: Story) => void;
  /** Called when the person abandons the form. */
  readonly onCancel: () => void;
};

/** The language tag the form starts with. */
const DEFAULT_LANGUAGE = 'en';

/** Field limits, mirroring the server's rules so the user is told early. */
const MAX_TITLE_LENGTH = 200;
const MAX_LOCATION_LENGTH = 100;
const LANGUAGE_PATTERN = /^[A-Za-z]{2,8}$/;

/**
 * Returns the first client-side validation problem, or undefined when the form is
 * acceptable. The server validates again and remains the source of truth.
 */
function validateForm(
  title: string,
  body: string,
  language: string,
  location: string,
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
    return 'The language must be 2 to 8 letters, such as en or tsonga.';
  }
  if (location.trim().length > MAX_LOCATION_LENGTH) {
    return `The place must be at most ${MAX_LOCATION_LENGTH} characters.`;
  }
  return undefined;
}

/**
 * The story composer.
 *
 * There is no author field: the author is the signed-in account, decided by the
 * server from the access token. The form therefore cannot publish as someone else.
 */
export default function CreateStoryScreen({
  token,
  onCreated,
  onCancel,
}: CreateStoryScreenProps): React.ReactElement {
  const [pillar, setPillar] = useState<Pillar>('wonder');
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [language, setLanguage] = useState(DEFAULT_LANGUAGE);
  const [location, setLocation] = useState('');
  const [mediaUrl, setMediaUrl] = useState('');
  const [sensitive, setSensitive] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);
  const [media, setMedia] = useState<readonly PickedMedia[]>([]);
  const [pickerVisible, setPickerVisible] = useState(false);
  // Set once the story has been created. If a media upload then fails, the story
  // already exists, so a retry reuses it rather than publishing a duplicate.
  const [createdStory, setCreatedStory] = useState<Story | undefined>(undefined);

  function addMedia(picked: PickedMedia): void {
    setMedia((current) => [...current, picked]);
  }

  function removeMedia(index: number): void {
    setMedia((current) => current.filter((_, i) => i !== index));
  }

  async function handleSubmit(): Promise<void> {
    const problem = validateForm(title, body, language, location);
    if (problem !== undefined) {
      setError(problem);
      return;
    }

    setError(undefined);
    setSubmitting(true);

    const payload: CreateStoryPayload = {
      pillar,
      language: language.trim().toLowerCase(),
      title: title.trim(),
      body,
      ...(location.trim() === '' ? {} : { approximate_location: location.trim() }),
      ...(mediaUrl.trim() === '' ? {} : { media_urls: [mediaUrl.trim()] }),
      sensitive,
    };

    try {
      // Create the story once. On a retry after a media failure it is reused.
      let story = createdStory;
      if (story === undefined) {
        const result = await storiesApi.createStory(token, payload);
        story = result.story;
        setCreatedStory(story);
      }

      // Upload the media sequentially: one request at a time, so a large video
      // is not competing for bandwidth with the next file (KNOT-ADR-032).
      for (const item of media) {
        await uploadStoryMedia(story.id, item.uri, item.mimeType, item.source, token, item.size, {
          ...(item.width === undefined ? {} : { width: item.width }),
          ...(item.height === undefined ? {} : { height: item.height }),
          ...(item.duration === undefined ? {} : { durationMs: Math.round(item.duration * 1000) }),
        });
      }

      onCreated(story);
    } catch (caught) {
      // A media failure leaves the story in place: it is already published, so it
      // is kept and the error shown rather than rolled back.
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

      <Text style={styles.title}>Tell a story</Text>

      <Text style={styles.label}>Media</Text>
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        style={styles.thumbnails}
        contentContainerStyle={styles.thumbnailsContent}
      >
        <Pressable
          style={styles.addMedia}
          onPress={() => setPickerVisible(true)}
          accessibilityRole="button"
          accessibilityLabel="Add media"
        >
          <Text style={styles.addMediaText}>+ Add media</Text>
        </Pressable>

        {media.map((item, index) => (
          <View key={`${item.uri}-${index}`} style={styles.thumbnail}>
            {item.type === 'image' ? (
              <Image style={styles.thumbnailImage} source={{ uri: item.uri }} />
            ) : (
              <View style={[styles.thumbnailImage, styles.videoPlaceholder]}>
                <Text style={styles.playIcon}>▶</Text>
              </View>
            )}
            {item.source === 'camera' ? <CameraCaptureBadge style={styles.badge} /> : null}
            <Pressable
              style={styles.removeButton}
              onPress={() => removeMedia(index)}
              accessibilityRole="button"
              accessibilityLabel="Remove media"
            >
              <Text style={styles.removeText}>×</Text>
            </Pressable>
          </View>
        ))}
      </ScrollView>

      <Text style={styles.label}>Pillar</Text>
      <View style={styles.pillars}>
        {PILLARS.map((option) => (
          <Pressable
            key={option}
            style={[styles.pillar, pillar === option ? styles.pillarSelected : null]}
            onPress={() => setPillar(option)}
          >
            <Text style={pillar === option ? styles.pillarTextSelected : styles.pillarText}>
              {option}
            </Text>
          </Pressable>
        ))}
      </View>

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

      <Text style={styles.label}>Language</Text>
      <TextInput
        style={styles.input}
        value={language}
        onChangeText={setLanguage}
        autoCapitalize="none"
        autoCorrect={false}
        placeholder="en"
        placeholderTextColor={colors.text.secondary}
      />

      <Text style={styles.label}>Approximate place (optional)</Text>
      <TextInput
        style={styles.input}
        value={location}
        onChangeText={setLocation}
        placeholder="Cape Town"
        placeholderTextColor={colors.text.secondary}
      />

      <Text style={styles.label}>One media link (optional)</Text>
      <TextInput
        style={styles.input}
        value={mediaUrl}
        onChangeText={setMediaUrl}
        autoCapitalize="none"
        autoCorrect={false}
        keyboardType="url"
        placeholder="https://example.com/photo.jpg"
        placeholderTextColor={colors.text.secondary}
      />

      <View style={styles.switchRow}>
        <Text style={styles.label}>Sensitive story</Text>
        <Switch value={sensitive} onValueChange={setSensitive} />
      </View>
      <Text style={styles.hint}>
        Mark a story sensitive when it should not be surfaced without care.
      </Text>

      {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

      <Pressable
        style={[styles.button, submitting ? styles.buttonDisabled : null]}
        onPress={handleSubmit}
        disabled={submitting}
      >
        <Text style={styles.buttonText}>{submitting ? 'Publishing…' : 'Publish story'}</Text>
      </Pressable>

      <MediaPickerSheet
        visible={pickerVisible}
        onClose={() => setPickerVisible(false)}
        onPicked={addMedia}
        onError={setError}
      />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  addMedia: {
    alignItems: 'center',
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderStyle: 'dashed',
    borderWidth: 1,
    height: 88,
    justifyContent: 'center',
    paddingHorizontal: spacing.md,
    width: 88,
  },
  addMediaText: {
    color: colors.text.brand,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.semiBold,
    textAlign: 'center',
  },
  badge: {
    position: 'absolute',
    right: spacing.xs,
    top: spacing.xs,
  },
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
    marginTop: spacing.xs,
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
    minHeight: 160,
  },
  pillar: {
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    marginRight: spacing.md,
    paddingHorizontal: spacing.lg,
    paddingVertical: 10,
  },
  pillarSelected: {
    backgroundColor: colors.brand.purple,
    borderColor: colors.brand.purple,
  },
  pillarText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  pillarTextSelected: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  pillars: {
    flexDirection: 'row',
    marginTop: 6,
  },
  playIcon: {
    color: colors.text.primary,
    fontSize: fontSizes.lg,
  },
  removeButton: {
    alignItems: 'center',
    backgroundColor: colors.bg.secondary,
    borderColor: colors.border.default,
    borderRadius: radius.pill,
    borderWidth: 1,
    height: 22,
    justifyContent: 'center',
    position: 'absolute',
    right: -6,
    top: -6,
    width: 22,
  },
  removeText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.bold,
    lineHeight: 18,
  },
  switchRow: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: spacing.md,
  },
  thumbnail: {
    height: 88,
    marginLeft: spacing.md,
    width: 88,
  },
  thumbnailImage: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    height: 88,
    width: 88,
  },
  thumbnails: {
    marginTop: 6,
  },
  thumbnailsContent: {
    paddingRight: spacing.sm,
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
  },
  videoPlaceholder: {
    alignItems: 'center',
    justifyContent: 'center',
  },
});

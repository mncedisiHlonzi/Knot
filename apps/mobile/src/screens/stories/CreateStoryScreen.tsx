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
import { DEFAULT_LANGUAGE_CODE } from '../../config/language';
import CameraCaptureBadge from '../../components/CameraCaptureBadge';
import LanguagePicker from '../../components/LanguagePicker';
import LocationPicker, { PickedPlace } from '../../components/LocationPicker';
import MediaPickerSheet, { PickedMedia } from '../../components/MediaPickerSheet';
import { isLanguageCode } from '../../data/languages';
import {
  MEDIA_LIMIT_MESSAGE,
  canAddMedia,
  mediaCountLabel,
  remainingMediaSlots,
} from '../../utils/mediaLimit';
import { colors, fontSizes, fontWeights, radius, spacing } from '../../theme';

type CreateStoryScreenProps = {
  /** The signed-in user's access token. The server derives the author from it. */
  readonly token: string;
  /** Called with the published story once it has been stored. */
  readonly onCreated: (story: Story) => void;
  /** Called when the person abandons the form. */
  readonly onCancel: () => void;
};

/** The language the form starts with. */
const DEFAULT_LANGUAGE = DEFAULT_LANGUAGE_CODE;

/** Field limits, mirroring the server's rules so the user is told early. */
const MAX_TITLE_LENGTH = 200;
const MAX_LOCATION_LENGTH = 100;

/**
 * Returns the first client-side validation problem, or undefined when the form is
 * acceptable. The server validates again and remains the source of truth.
 */
function validateForm(
  title: string,
  body: string,
  language: string,
  locationText: string,
  selectedPlace: PickedPlace | null,
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
    return 'Choose the language you are telling this story in.';
  }
  if (locationText.trim().length > MAX_LOCATION_LENGTH) {
    return `The place must be at most ${MAX_LOCATION_LENGTH} characters.`;
  }
  // A place must be chosen from the suggestions, so every located story has a
  // coordinate the map can plot (KNOT-ADR-036). Typing without selecting is a
  // validation failure, not a silent free-text place.
  if (locationText.trim() !== '' && selectedPlace === null) {
    return 'Please select a place from the suggestions.';
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
  const [locationText, setLocationText] = useState('');
  const [selectedPlace, setSelectedPlace] = useState<PickedPlace | null>(null);
  const [mediaUrl, setMediaUrl] = useState('');
  const [sensitive, setSensitive] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);
  const [media, setMedia] = useState<readonly PickedMedia[]>([]);
  const [pickerVisible, setPickerVisible] = useState(false);
  // The inline notice shown when a pick is cut down to the free slots.
  const [mediaNotice, setMediaNotice] = useState<string | undefined>(undefined);
  // Set once the story has been created. If a media upload then fails, the story
  // already exists, so a retry reuses it rather than publishing a duplicate.
  const [createdStory, setCreatedStory] = useState<Story | undefined>(undefined);

  /** The story is full: another item would exceed the server's cap (KNOT-ADR-054). */
  const atMediaCap = !canAddMedia(media.length);

  /**
   * Adds every picked item that still fits, and tells the reader when a pick was
   * cut down.
   *
   * The cap is re-checked here because the picker's selection limit is only a
   * hint: a library that ignores it can still return more than the free slots, so
   * the client truncates and the server remains the final authority (KNOT-ADR-054).
   */
  function addMedia(picked: readonly PickedMedia[]): void {
    if (picked.length === 0) {
      return;
    }

    const free = remainingMediaSlots(media.length);
    if (picked.length > free) {
      setMedia([...media, ...picked.slice(0, free)]);
      setMediaNotice(MEDIA_LIMIT_MESSAGE);
      return;
    }

    setMedia([...media, ...picked]);
    setMediaNotice(undefined);
  }

  function removeMedia(index: number): void {
    setMedia((current) => current.filter((_, i) => i !== index));
    // Removing frees a slot, so a previous cap notice no longer applies.
    setMediaNotice(undefined);
  }

  async function handleSubmit(): Promise<void> {
    const problem = validateForm(title, body, language, locationText, selectedPlace);
    if (problem !== undefined) {
      setError(problem);
      return;
    }

    setError(undefined);
    setSubmitting(true);

    const payload: CreateStoryPayload = {
      pillar,
      language: language,
      title: title.trim(),
      body,
      ...(locationText.trim() === '' ? {} : { approximate_location: locationText.trim() }),
      ...(selectedPlace === null
        ? {}
        : {
            latitude: selectedPlace.latitude,
            longitude: selectedPlace.longitude,
            ...(selectedPlace.country === null ? {} : { place_country: selectedPlace.country }),
          }),
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
          style={[styles.addMedia, atMediaCap ? styles.buttonDisabled : null]}
          onPress={() => setPickerVisible(true)}
          disabled={atMediaCap}
          accessibilityRole="button"
          accessibilityLabel="Add media"
          accessibilityState={{ disabled: atMediaCap }}
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
      <Text style={styles.mediaCount}>{mediaCountLabel(media.length)}</Text>
      {mediaNotice !== undefined ? <Text style={styles.mediaNotice}>{mediaNotice}</Text> : null}

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
      <LanguagePicker
        mode="single"
        selected={language}
        onSelect={(chosen) => setLanguage(chosen.code)}
        onClear={() => setLanguage('')}
        placeholder="Search, e.g. Zulu or zul"
      />

      <Text style={styles.label}>Approximate place (optional)</Text>
      <LocationPicker
        text={locationText}
        selected={selectedPlace}
        onChangeText={(next) => {
          setLocationText(next);
          // Typing after a selection invalidates it until a new one is chosen.
          setSelectedPlace(null);
        }}
        onSelect={(place) => {
          setSelectedPlace(place);
          setLocationText(place.place);
        }}
        onClear={() => {
          setLocationText('');
          setSelectedPlace(null);
        }}
        placeholder="Search for a place"
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
        maxSelection={remainingMediaSlots(media.length)}
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
  // The "N / 10" counter under the strip, and the notice when a pick was cut
  // down to the free slots (KNOT-ADR-054).
  mediaCount: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: spacing.xs,
  },
  mediaNotice: {
    color: colors.state.error,
    fontSize: fontSizes.sm,
    marginTop: spacing.xs,
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

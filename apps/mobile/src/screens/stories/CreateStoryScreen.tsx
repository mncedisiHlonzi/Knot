import React, { useState } from 'react';
import { Image, Pressable, ScrollView, StyleSheet, Switch, Text, View } from 'react-native';
import { FilmStrip, Plus, X } from 'phosphor-react-native';

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
import Button from '../../components/Button';
import CameraCaptureBadge from '../../components/CameraCaptureBadge';
import Chip from '../../components/Chip';
import Header from '../../components/Header';
import Input from '../../components/Input';
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
import { colors, layout, radius, spacing, typeScale } from '../../theme';

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

/** The media tile's edge length, so a picked item and the "add" slot line up. */
const THUMBNAIL_SIZE = 88;

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

/** The word a pillar chip shows, so the chip reads as a label rather than a code. */
function pillarLabel(pillar: Pillar): string {
  return pillar === 'wonder' ? 'Wonder' : 'Heritage';
}

/**
 * The story composer.
 *
 * There is no author field: the author is the signed-in account, decided by the
 * server from the access token. The form therefore cannot publish as someone else.
 *
 * It is built from the design system: the shared `Header` (a Cancel, because the
 * screen is a tab rather than a pushed page), the underline `Input`s, the pillar
 * as a row of `Chip`s, the existing language, place, and media pickers, and a
 * full-width primary `Button` to publish. The headline is the screen's own, in
 * `typeScale.h1`, so the bar stays quiet and the form announces itself the way the
 * Discovery sheet does (KNOT-020g-fix12).
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
    <View style={styles.container}>
      {/* The screen's own bar. The form is reached from the tab bar, so leaving it
          is a Cancel rather than a back arrow (KNOT-020g-fix12). */}
      <Header
        left={
          <Pressable
            style={styles.cancel}
            onPress={onCancel}
            hitSlop={spacing.sm}
            accessibilityRole="button"
            accessibilityLabel="Cancel the story"
          >
            <Text style={[typeScale.button, styles.cancelText]}>Cancel</Text>
          </Pressable>
        }
      />

      <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        <Text style={[typeScale.h1, styles.headline]}>Tell a story.</Text>
        <Text style={[typeScale.body, styles.subtitle]}>
          Say what happened in your own words. It is published under your name.
        </Text>

        <Text style={[typeScale.overline, styles.label]}>Media</Text>
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
            <Plus color={colors.brand.primary} size={20} weight="bold" />
            <Text style={[typeScale.caption, styles.addMediaText]}>Add media</Text>
          </Pressable>

          {media.map((item, index) => (
            <View key={`${item.uri}-${index}`} style={styles.thumbnail}>
              {item.type === 'image' ? (
                <Image style={styles.thumbnailImage} source={{ uri: item.uri }} />
              ) : (
                <View style={[styles.thumbnailImage, styles.videoPlaceholder]}>
                  <FilmStrip color={colors.text.tertiary} size={24} />
                </View>
              )}
              {item.source === 'camera' ? <CameraCaptureBadge style={styles.badge} /> : null}
              <Pressable
                style={styles.removeButton}
                onPress={() => removeMedia(index)}
                hitSlop={spacing.sm}
                accessibilityRole="button"
                accessibilityLabel="Remove media"
              >
                <X color={colors.text.onBrand} size={12} weight="bold" />
              </Pressable>
            </View>
          ))}
        </ScrollView>
        <Text style={[typeScale.caption, styles.mediaCount]}>{mediaCountLabel(media.length)}</Text>
        {mediaNotice === undefined ? null : (
          <Text style={[typeScale.caption, styles.mediaNotice]}>{mediaNotice}</Text>
        )}

        <Text style={[typeScale.overline, styles.label]}>Pillar</Text>
        <View style={styles.chipRow}>
          {PILLARS.map((option) => (
            <Chip
              key={option}
              label={pillarLabel(option)}
              selected={pillar === option}
              onPress={() => setPillar(option)}
            />
          ))}
        </View>

        <View style={styles.field}>
          <Input
            label="Title"
            value={title}
            onChangeText={setTitle}
            accessibilityLabel="The story's title"
            placeholder="A short headline"
          />
        </View>

        <View style={styles.field}>
          <Input
            label="Story"
            value={body}
            onChangeText={setBody}
            multiline
            accessibilityLabel="The story"
            placeholder="Tell it the way you would tell it out loud"
          />
        </View>

        <Text style={[typeScale.overline, styles.label]}>Language</Text>
        <LanguagePicker
          mode="single"
          selected={language}
          onSelect={(chosen) => setLanguage(chosen.code)}
          onClear={() => setLanguage('')}
          placeholder="Search, e.g. Zulu or zul"
        />

        <Text style={[typeScale.overline, styles.label]}>Approximate place (optional)</Text>
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

        <View style={styles.field}>
          <Input
            label="One media link (optional)"
            value={mediaUrl}
            onChangeText={setMediaUrl}
            keyboardType="url"
            autoCapitalize="none"
            autoCorrect={false}
            accessibilityLabel="A link to one media item"
            placeholder="https://example.com/photo.jpg"
          />
        </View>

        <View style={styles.switchRow}>
          <View style={styles.switchText}>
            <Text style={[typeScale.overline, styles.switchLabel]}>Sensitive story</Text>
            <Text style={[typeScale.caption, styles.hint]}>
              Mark a story sensitive when it should not be surfaced without care.
            </Text>
          </View>
          <Switch value={sensitive} onValueChange={setSensitive} />
        </View>

        {error === undefined ? null : <Text style={[typeScale.bodyS, styles.error]}>{error}</Text>}

        <View style={styles.submit}>
          <Button
            variant="primary"
            fullWidth
            label="Publish story"
            loading={submitting}
            accessibilityLabel="Publish the story"
            onPress={() => {
              void handleSubmit();
            }}
          />
        </View>

        <MediaPickerSheet
          visible={pickerVisible}
          onClose={() => setPickerVisible(false)}
          onPicked={addMedia}
          onError={setError}
          maxSelection={remainingMediaSlots(media.length)}
        />
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  // The "add media" tile: a dashed outline in the hairline ink, so it reads as an
  // empty slot rather than as a button, with the indigo plus as its one strong mark.
  addMedia: {
    alignItems: 'center',
    borderColor: colors.border.hairline,
    borderRadius: radius.md,
    borderStyle: 'dashed',
    borderWidth: 1,
    gap: spacing.xs,
    height: THUMBNAIL_SIZE,
    justifyContent: 'center',
    paddingHorizontal: spacing.sm,
    width: THUMBNAIL_SIZE,
  },
  addMediaText: {
    color: colors.brand.primary,
    textAlign: 'center',
  },
  badge: {
    position: 'absolute',
    left: spacing.xs,
    top: spacing.xs,
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  // The header's Cancel: a text control rather than a glyph, because leaving a tab
  // is not a "close".
  cancel: {
    paddingRight: spacing.sm,
  },
  cancelText: {
    color: colors.brand.primary,
  },
  chipRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
  },
  container: {
    backgroundColor: colors.bg.canvas,
    flex: 1,
  },
  content: {
    paddingBottom: spacing.xxxl,
    paddingHorizontal: layout.screenPaddingReading,
    paddingTop: spacing.sm,
  },
  error: {
    color: colors.state.danger,
    marginTop: spacing.md,
  },
  // One labelled field, with the inquiry form's vertical rhythm.
  field: {
    marginTop: spacing.lg,
  },
  headline: {
    color: colors.text.primary,
  },
  hint: {
    color: colors.text.tertiary,
    marginTop: spacing.xs,
  },
  label: {
    color: colors.text.secondary,
    marginBottom: spacing.sm,
    marginTop: spacing.xl,
  },
  // The "N / 10" counter under the strip, and the notice when a pick was cut
  // down to the free slots (KNOT-ADR-054).
  mediaCount: {
    color: colors.text.tertiary,
    marginTop: spacing.sm,
  },
  mediaNotice: {
    color: colors.state.danger,
    marginTop: spacing.xs,
  },
  // The thumbnail's remove control: a filled indigo dot on the tile's corner, with
  // hit slop taking it past the 44-point target the 22-point dot cannot reach on
  // its own.
  removeButton: {
    alignItems: 'center',
    backgroundColor: colors.brand.primary,
    borderRadius: radius.pill,
    height: 22,
    justifyContent: 'center',
    position: 'absolute',
    right: -6,
    top: -6,
    width: 22,
  },
  submit: {
    marginTop: spacing.xl,
  },
  subtitle: {
    color: colors.text.secondary,
    marginTop: spacing.xs,
  },
  switchRow: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: spacing.lg,
    justifyContent: 'space-between',
    marginTop: spacing.xl,
  },
  switchText: {
    flex: 1,
  },
  // The switch's own label: the row already carries the top margin, so this one
  // only needs the gap beneath it.
  switchLabel: {
    color: colors.text.secondary,
    marginBottom: spacing.xs,
  },
  thumbnail: {
    height: THUMBNAIL_SIZE,
    marginLeft: spacing.md,
    width: THUMBNAIL_SIZE,
  },
  thumbnailImage: {
    backgroundColor: colors.bg.sunken,
    borderRadius: radius.md,
    height: THUMBNAIL_SIZE,
    width: THUMBNAIL_SIZE,
  },
  thumbnails: {
    marginTop: spacing.sm,
  },
  thumbnailsContent: {
    paddingRight: spacing.sm,
  },
  videoPlaceholder: {
    alignItems: 'center',
    justifyContent: 'center',
  },
});

import React, { useState } from 'react';
import {
  Alert,
  Image,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import { FilmStrip, Flag, Globe, ImageSquare, MapPin, Plus, X } from 'phosphor-react-native';

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
import LanguagePicker from '../../components/LanguagePicker';
import LocationPicker, { PickedPlace } from '../../components/LocationPicker';
import MediaPickerSheet, { PickedMedia } from '../../components/MediaPickerSheet';
import { isLanguageCode, languageName } from '../../data/languages';
import {
  MAX_MEDIA_PER_STORY,
  MEDIA_LIMIT_MESSAGE,
  canAddMedia,
  remainingMediaSlots,
} from '../../utils/mediaLimit';
import { layout, radius, spacing, typeScale, useTheme, useThemedStyles } from '../../theme';
import type { ThemeColors } from '../../theme';
import { withPendingMedia } from './storyPolling';
import type { MediaUploadProgress } from './mediaUpload';

type CreateStoryScreenProps = {
  /** The signed-in user's access token. The server derives the author from it. */
  readonly token: string;
  /** Called with the published story once it has been stored. */
  readonly onCreated: (story: Story) => void;
  /** Called when the person abandons the form. */
  readonly onCancel: () => void;
  /**
   * Called as the background upload of the just-published story's media advances,
   * and once more if one of them fails.
   *
   * The bytes now go straight from the device to the object store (KNOT-040), so
   * the composer is already closed while they travel; the feed's card for the new
   * story is what shows this. The screen reports it upwards rather than drawing it
   * itself, because there is no screen left when the first byte moves.
   */
  readonly onUploadProgress?: (storyId: string, progress: MediaUploadProgress) => void;
};

/** The language the form starts with. */
const DEFAULT_LANGUAGE = DEFAULT_LANGUAGE_CODE;

/** Field limits, mirroring the server's rules so the user is told early. */
const MAX_TITLE_LENGTH = 200;
const MAX_LOCATION_LENGTH = 100;

/** The media tile's edge length, so a picked item and the "add" slot line up. */
const THUMBNAIL_SIZE = 88;

/** The metadata chips' glyph size. */
const CHIP_ICON_SIZE = 14;

/** The footer glyphs' size. */
const FOOTER_ICON_SIZE = 20;

/** The title field's prompt. */
const TITLE_PLACEHOLDER = 'Give your story a headline';

/** The body field's prompt. */
const BODY_PLACEHOLDER = 'Tell it the way you would tell it out loud';

/**
 * The footer's status line.
 *
 * The design draws "Draft saved" here. Nothing on this screen is persisted — a
 * draft exists only in memory until Publish succeeds — so the copy says what is
 * actually true rather than promising a save that would not survive a restart
 * (KNOT-020k-fix14).
 */
const DRAFT_STATUS = 'Not published yet';

/** The headline of the notice shown when a story is published (KNOT-037-fix12). */
const PUBLISHED_TITLE = 'Story published';

/** The headline shown when a published story's media could not be uploaded. */
const UPLOAD_FAILED_TITLE = 'Media upload failed';

/**
 * What publishing tells the reader.
 *
 * The story exists the moment the composer closes, but its pictures and videos are
 * still being encoded by the backend, so the sentence says so instead of implying
 * that everything is already on screen (KNOT-037-fix12).
 */
const PROCESSING_NOTICE =
  'Your story is being processed. We will show the media here as soon as it is ready.';

/**
 * Uploads the composer's media after the reader has left it.
 *
 * Publishing closes the composer before these requests finish — the story exists
 * and the feed is already showing it (KNOT-037-fix12) — so there is no screen left
 * to report a failure to; the caller shows it and the card carries the progress.
 * Each item is sent one at a time, so a large video never competes with the next
 * file for the same uplink (KNOT-ADR-032), and every item's bytes go straight to
 * the object store rather than through the API (KNOT-040).
 *
 * Returns the first failure's message, or undefined when every item was attached.
 */
async function uploadPickedMedia(
  storyId: string,
  media: readonly PickedMedia[],
  token: string,
  onProgress?: (progress: MediaUploadProgress) => void,
): Promise<string | undefined> {
  const total = media.length;
  let completed = 0;
  let failure: string | undefined;

  if (total > 0) {
    onProgress?.({ completed, total, failed: false });
  }

  for (const item of media) {
    try {
      await uploadStoryMedia(storyId, item.uri, item.mimeType, item.source, token, item.size, {
        ...(item.width === undefined ? {} : { width: item.width }),
        ...(item.height === undefined ? {} : { height: item.height }),
        ...(item.duration === undefined ? {} : { durationMs: Math.round(item.duration * 1000) }),
      });
    } catch (caught) {
      const message = describeError(caught);
      console.warn('[create] media upload failed after publishing', message);
      if (failure === undefined) {
        failure = message;
      }
    }

    completed += 1;
    onProgress?.({ completed, total, failed: failure !== undefined });
  }

  return failure;
}

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
 * The layout is media-first and text-first at once (KNOT-020k-fix14): a bar with a
 * close and the one action, a metadata row of pillar / language / place, two
 * BORDERLESS fields — the headline at `h1`, the story at `reading` — and the media
 * strip under them, closed by a footer that states where the draft stands. No
 * label sits above a field and no underline sits under one, so the words and the
 * pictures are the only things with weight on the page.
 */
export default function CreateStoryScreen({
  token,
  onCreated,
  onCancel,
  onUploadProgress,
}: CreateStoryScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [pillar, setPillar] = useState<Pillar>('wonder');
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [language, setLanguage] = useState(DEFAULT_LANGUAGE);
  const [locationText, setLocationText] = useState('');
  const [selectedPlace, setSelectedPlace] = useState<PickedPlace | null>(null);
  const [error, setError] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);
  const [media, setMedia] = useState<readonly PickedMedia[]>([]);
  const [pickerVisible, setPickerVisible] = useState(false);
  // Which of the two metadata pickers the chips have opened, if any.
  const [openPicker, setOpenPicker] = useState<'language' | 'place' | undefined>(undefined);
  // The inline notice shown when a pick is cut down to the free slots.
  const [mediaNotice, setMediaNotice] = useState<string | undefined>(undefined);

  /** The story is full: another item would exceed the server's cap (KNOT-ADR-054). */
  const atMediaCap = !canAddMedia(media.length);

  /** Shows the picker a chip names, or closes it when it is the one already open. */
  function togglePicker(which: 'language' | 'place'): () => void {
    return () => {
      setOpenPicker(openPicker === which ? undefined : which);
    };
  }

  /** The footer's flag: the pillar is a two-way choice, so it flips (KNOT-020k-fix14). */
  function switchPillar(): void {
    setPillar((current) => (current === 'wonder' ? 'heritage' : 'wonder'));
  }

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
    };

    try {
      // Only the story itself is awaited. It is one small JSON request, and its id
      // is what the feed needs in order to insert the card and poll it; the media
      // is uploaded afterwards, so a 20 MiB video no longer holds the reader on
      // this screen while the bytes cross the wire (KNOT-037-fix12).
      const result = await storiesApi.createStory(token, payload);
      const story = result.story;

      Alert.alert(PUBLISHED_TITLE, PROCESSING_NOTICE);

      // The story is handled over with local `processing` placeholders, so the
      // feed can draw it at once and fill it in when the server reports the real
      // rows (KNOT-037-fix8).
      onCreated(withPendingMedia(story, media));

      // The bytes follow the story instead of blocking it. The bytes go straight
      // to the object store, then the API is told where they are, answers 202, and
      // the feed polls the story until the worker has produced the renditions
      // (KNOT-037-fix12, KNOT-040).
      void uploadPickedMedia(story.id, media, token, (progress) => {
        onUploadProgress?.(story.id, progress);
      }).then((failure) => {
        if (failure !== undefined) {
          Alert.alert(UPLOAD_FAILED_TITLE, failure);
        }
      });
    } catch (caught) {
      // The story was NOT created, so the composer stays open with the error and
      // the reader can try again without losing what they wrote.
      setError(describeError(caught));
      setSubmitting(false);
    }
  }

  return (
    <View style={styles.container}>
      {/* Two controls and nothing else: leave, or publish. The screen's own
          headline is gone — the story's headline is the first field now
          (KNOT-020k-fix14). */}
      <Header
        left={
          <Pressable
            style={styles.iconButton}
            onPress={onCancel}
            accessibilityRole="button"
            accessibilityLabel="Close the composer"
            testID="create-close"
          >
            <X color={colors.text.primary} size={22} weight="bold" />
          </Pressable>
        }
        right={
          <Button
            variant="primary"
            label="Publish"
            loading={submitting}
            accessibilityLabel="Publish the story"
            onPress={() => {
              void handleSubmit();
            }}
            testID="create-publish"
          />
        }
      />

      <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        {/* What the story is filed under, written in, and about: the pillar pair on
            the left, the two pickers on the right (KNOT-020k-fix14). */}
        <View style={styles.metaRow}>
          <View style={styles.pillarGroup}>
            {PILLARS.map((option) => (
              <Chip
                key={option}
                label={pillarLabel(option)}
                textScale="button"
                selected={pillar === option}
                onPress={() => setPillar(option)}
                testID={`create-pillar-${option}`}
              />
            ))}
          </View>
          <View style={styles.metaEnd}>
            <Chip
              label={languageName(language)}
              variant="outline"
              icon={<Globe color={colors.text.tertiary} size={CHIP_ICON_SIZE} weight="bold" />}
              onPress={togglePicker('language')}
              testID="create-language-chip"
            />
            <Chip
              label={locationText.trim() === '' ? 'Place' : locationText.trim()}
              variant="outline"
              icon={<MapPin color={colors.text.tertiary} size={CHIP_ICON_SIZE} weight="bold" />}
              onPress={togglePicker('place')}
              testID="create-place-chip"
            />
          </View>
        </View>

        {openPicker === 'language' ? (
          <View style={styles.picker} testID="create-language-picker">
            <LanguagePicker
              mode="single"
              selected={language}
              onSelect={(chosen) => {
                setLanguage(chosen.code);
                setOpenPicker(undefined);
              }}
              onClear={() => setLanguage('')}
              placeholder="Search, e.g. Zulu or zul"
            />
          </View>
        ) : null}

        {openPicker === 'place' ? (
          <View style={styles.picker} testID="create-place-picker">
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
                setOpenPicker(undefined);
              }}
              onClear={() => {
                setLocationText('');
                setSelectedPlace(null);
              }}
              placeholder="Search for a place"
            />
          </View>
        ) : null}

        {/* Borderless fields: the headline at `h1`, the story at `reading`, with no
            label above and no underline below (KNOT-020k-fix14). */}
        <TextInput
          style={[typeScale.h1, styles.title]}
          value={title}
          onChangeText={setTitle}
          placeholder={TITLE_PLACEHOLDER}
          placeholderTextColor={colors.text.disabled}
          accessibilityLabel="The story's title"
          multiline
          testID="create-title"
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
          testID="create-body"
        />

        {/* The media strip: the count first, then the tiles, then the dashed slot
            that adds the next one (KNOT-020k-fix14). */}
        <Text style={[typeScale.overline, styles.mediaLabel]} testID="create-media-label">
          {`Media · ${media.length} of ${MAX_MEDIA_PER_STORY}`}
        </Text>
        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          style={styles.thumbnails}
          contentContainerStyle={styles.thumbnailsContent}
        >
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

          <Pressable
            style={[styles.addMedia, atMediaCap ? styles.buttonDisabled : null]}
            onPress={() => setPickerVisible(true)}
            disabled={atMediaCap}
            accessibilityRole="button"
            accessibilityLabel="Add media"
            accessibilityState={{ disabled: atMediaCap }}
            testID="create-add-media"
          >
            <Plus color={colors.text.primary} size={20} weight="bold" />
          </Pressable>
        </ScrollView>
        {mediaNotice === undefined ? null : (
          <Text style={[typeScale.caption, styles.mediaNotice]}>{mediaNotice}</Text>
        )}
        {error === undefined ? null : <Text style={[typeScale.bodyS, styles.error]}>{error}</Text>}
      </ScrollView>

      <View style={styles.footer} testID="create-footer">
        {/* The three quick marks: the image opens the media picker, the pin the
            place picker, and the flag flips the pillar — the same two-way choice the
            chips above make (KNOT-020k-fix14). */}
        <View style={styles.footerIcons}>
          <Pressable
            style={styles.footerButton}
            onPress={() => setPickerVisible(true)}
            disabled={atMediaCap}
            accessibilityRole="button"
            accessibilityLabel="Add media"
            accessibilityState={{ disabled: atMediaCap }}
            testID="create-footer-media"
          >
            <ImageSquare color={colors.text.primary} size={FOOTER_ICON_SIZE} weight="bold" />
          </Pressable>
          <Pressable
            style={styles.footerButton}
            onPress={togglePicker('place')}
            accessibilityRole="button"
            accessibilityLabel="Choose a place"
            testID="create-footer-place"
          >
            <MapPin color={colors.text.primary} size={FOOTER_ICON_SIZE} weight="bold" />
          </Pressable>
          <Pressable
            style={styles.footerButton}
            onPress={switchPillar}
            accessibilityRole="button"
            accessibilityLabel={`Switch to ${pillar === 'wonder' ? 'Heritage' : 'Wonder'}`}
            testID="create-footer-pillar"
          >
            <Flag color={colors.text.primary} size={FOOTER_ICON_SIZE} weight="bold" />
          </Pressable>
        </View>

        {/* The design draws "Draft saved" here. Nothing is persisted until Publish
            succeeds, so the line says what is actually true (KNOT-020k-fix14). */}
        <Text style={[typeScale.caption, styles.footerStatus]}>{DRAFT_STATUS}</Text>
      </View>

      <MediaPickerSheet
        visible={pickerVisible}
        onClose={() => setPickerVisible(false)}
        onPicked={addMedia}
        onError={setError}
        maxSelection={remainingMediaSlots(media.length)}
      />
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    // The "add media" tile: a dashed outline in the hairline ink, so it reads as an
    // empty slot rather than as a button, with the plus as its one strong mark.
    addMedia: {
      alignItems: 'center',
      borderColor: colors.border.hairline,
      borderRadius: radius.md,
      borderStyle: 'dashed',
      borderWidth: 1,
      height: THUMBNAIL_SIZE,
      justifyContent: 'center',
      width: THUMBNAIL_SIZE,
    },
    badge: {
      position: 'absolute',
      left: spacing.xs,
      top: spacing.xs,
    },
    // The story's body: tall enough to read as a page, with its text at the top.
    body: {
      color: colors.text.primary,
      marginTop: spacing.lg,
      minHeight: 180,
      paddingVertical: 0,
    },
    buttonDisabled: {
      opacity: 0.5,
    },
    container: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    content: {
      flexGrow: 1,
      paddingBottom: spacing.xxl,
      paddingHorizontal: layout.screenPaddingReading,
      paddingTop: spacing.md,
    },
    error: {
      color: colors.state.danger,
      marginTop: spacing.md,
    },
    // The bottom bar: the quick marks on the left, the draft's status on the right.
    footer: {
      alignItems: 'center',
      borderTopColor: colors.border.hairline,
      borderTopWidth: 1,
      flexDirection: 'row',
      justifyContent: 'space-between',
      paddingHorizontal: layout.screenPaddingReading,
      paddingVertical: spacing.sm,
    },
    footerButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    footerIcons: {
      alignItems: 'center',
      flexDirection: 'row',
      marginLeft: -spacing.md,
    },
    footerStatus: {
      color: colors.text.tertiary,
    },
    // A 44-point tap target for the bar's close control.
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    // The media strip's label: the count, above the tiles.
    mediaLabel: {
      color: colors.text.secondary,
      marginTop: spacing.xl,
    },
    mediaNotice: {
      color: colors.state.danger,
      marginTop: spacing.xs,
    },
    // The metadata row: the pillar pair on the left, the two picker chips right.
    // It wraps rather than clipping, so a long place name cannot push a chip off
    // the screen (KNOT-020k-fix14).
    metaEnd: {
      flexDirection: 'row',
      flexShrink: 1,
      gap: spacing.sm,
    },
    metaRow: {
      alignItems: 'center',
      flexDirection: 'row',
      flexWrap: 'wrap',
      gap: spacing.sm,
      justifyContent: 'space-between',
    },
    // A revealed picker keeps its own look; it only needs air around it.
    picker: {
      marginTop: spacing.md,
    },
    pillarGroup: {
      flexDirection: 'row',
      gap: spacing.sm,
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
    thumbnail: {
      height: THUMBNAIL_SIZE,
      marginRight: spacing.sm,
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
    // The headline: the same step a title takes everywhere, because on this screen
    // it IS the title (KNOT-020k-fix14).
    title: {
      color: colors.text.primary,
      marginTop: spacing.xl,
      paddingVertical: 0,
    },
    videoPlaceholder: {
      alignItems: 'center',
      justifyContent: 'center',
    },
  });

import React, { useState } from 'react';
import {
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import { Globe, ImageSquare, MapPin, X } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { inquiriesApi } from '../../api/inquiries';
import Button from '../../components/Button';
import Chip from '../../components/Chip';
import Header from '../../components/Header';
import LanguagePicker from '../../components/LanguagePicker';
import LocationPicker, { PickedPlace } from '../../components/LocationPicker';
import { languageName } from '../../data/languages';
import { layout, spacing, typeScale, useTheme, useThemedStyles } from '../../theme';
import type { ThemeColors } from '../../theme';
import { inquiryAudienceLabel, inquiryDraftError } from '../../utils/inquiry';

/** The title field's prompt: a real question, asked the way a person asks it. */
const TITLE_PLACEHOLDER = 'Why do the older women wear red ribbons in their braids?';

/** The body field's prompt, continuing the question above. */
const BODY_PLACEHOLDER = 'At the festival almost every woman...';

/** The metadata chips' glyph size. */
const CHIP_ICON_SIZE = 14;

/** The footer glyph's size. */
const FOOTER_ICON_SIZE = 20;

/**
 * The footer's routing note.
 *
 * Naming a place lets the note say exactly who is told; with no place it keeps the
 * longer honest line, because asking then notifies nobody and the copy must not
 * imply otherwise (KNOT-020k-fix14).
 */
function audienceNote(place: string): string {
  return place === '' ? inquiryAudienceLabel('') : `People rooted in ${place} will be notified.`;
}

type AskInquiryScreenProps = {
  /** The signed-in user's access token. */
  readonly token: string;
  /** The language to start with, from the user's preferences. */
  readonly defaultLanguage: string;
  /**
   * The place the person was already looking at when they chose to ask, if any.
   * The Inquiries hero hands its place field over, so the form opens where the
   * thought began (KNOT-020k-fix9). It seeds the typed text only: a typed place is
   * not a resolved coordinate, so the picker still owns the structured pair.
   */
  readonly initialPlace?: string;
  /**
   * The opening words a starter chip handed over, if any — a beginning for the
   * title field, not a finished question (KNOT-020k-fix9).
   */
  readonly initialTitle?: string;
  /** Called with the created inquiry's id once the server has stored it. */
  readonly onAsked: (inquiryId: string) => void;
  /** Called when the person abandons the form. */
  readonly onCancel: () => void;
};

/**
 * Asks a question about a place.
 *
 * It is reached from the Inquiries tab rather than from Create, because Create is
 * a story composer and a question is not a story (KNOT-ADR-058).
 *
 * The screen is deliberately text-first (KNOT-020k-fix14): a bar with a close and
 * the one action, two outlined metadata chips that open the existing pickers, then
 * two BORDERLESS fields — the question at `h1`, the detail at `reading` — so the
 * writing is the only thing with weight on the page. There is no label above a
 * field and no underline under one: the placeholder is the prompt, and the text a
 * person types replaces it.
 *
 * The place is what makes an inquiry different from a story: naming one routes the
 * question to the first Rooted people there, and the footer says so before
 * anything is sent. Leaving it blank is allowed — the question is still public, it
 * is simply not announced to anyone.
 *
 * The asker is taken from the access token by the server; there is no author field
 * and no anonymous option, because every inquiry is attributed (KNOT-ADR-055).
 */
export default function AskInquiryScreen({
  token,
  defaultLanguage,
  initialPlace,
  initialTitle,
  onAsked,
  onCancel,
}: AskInquiryScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  // A seeded title is only a beginning: the field is editable and the person is
  // expected to finish the sentence (KNOT-020k-fix9).
  const [title, setTitle] = useState(initialTitle ?? '');
  const [body, setBody] = useState('');
  const [language, setLanguage] = useState(defaultLanguage);
  const [locationText, setLocationText] = useState(initialPlace ?? '');
  const [selectedPlace, setSelectedPlace] = useState<PickedPlace | null>(null);
  // Which picker the metadata row has opened, if any. The pickers themselves are
  // unchanged shared components; the chip is only the way in (KNOT-020k-fix14).
  const [openPicker, setOpenPicker] = useState<'language' | 'place' | undefined>(undefined);

  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  // The place that will actually be sent. A selection wins over the typed text,
  // because only a selection carries the coordinate the geocoder resolved.
  const place = selectedPlace?.place ?? locationText.trim();

  /** Shows the picker a chip names, or closes it when it is the one already open. */
  function togglePicker(which: 'language' | 'place'): () => void {
    return () => {
      setOpenPicker(openPicker === which ? undefined : which);
    };
  }

  async function handleSubmit(): Promise<void> {
    const validation = inquiryDraftError({ title, body, language });
    if (validation !== undefined) {
      setError(validation);
      return;
    }

    setSubmitting(true);
    setError(undefined);

    try {
      const created = await inquiriesApi.createInquiry(token, {
        title: title.trim(),
        body: body.trim(),
        language,
        ...(place === '' ? {} : { place }),
        // The structured place data is only sent when the geocoder actually
        // resolved the place; the pair goes together or not at all.
        ...(selectedPlace === null
          ? {}
          : {
              latitude: selectedPlace.latitude,
              longitude: selectedPlace.longitude,
              ...(selectedPlace.country === null ? {} : { place_country: selectedPlace.country }),
            }),
      });
      onAsked(created.inquiry.id);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <KeyboardAvoidingView
      style={styles.container}
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
    >
      {/* Two controls and nothing else: leave, or ask. The screen's own title is
          gone, because the question the person is writing is the title
          (KNOT-020k-fix14). */}
      <Header
        left={
          <Pressable
            style={styles.iconButton}
            onPress={onCancel}
            accessibilityRole="button"
            accessibilityLabel="Close"
            testID="ask-close"
          >
            <X color={colors.text.primary} size={22} weight="bold" />
          </Pressable>
        }
        right={
          <Button
            variant="primary"
            label="Ask"
            loading={submitting}
            accessibilityLabel="Ask your question"
            onPress={() => {
              void handleSubmit();
            }}
            testID="ask-submit"
          />
        }
      />

      <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        {/* The metadata: what the question is written in, and where it is about.
            Both open the pickers the rest of the app uses, so there is one place a
            language or a place is chosen (KNOT-020k-fix14). */}
        <View style={styles.chips}>
          <Chip
            label={language === '' ? 'Language' : languageName(language)}
            variant="outline"
            selected={language !== ''}
            icon={<Globe color={colors.text.tertiary} size={CHIP_ICON_SIZE} weight="bold" />}
            onPress={togglePicker('language')}
            testID="ask-language-chip"
          />
          <Chip
            label={place === '' ? 'Place' : place}
            variant="outline"
            selected={place !== ''}
            icon={<MapPin color={colors.text.tertiary} size={CHIP_ICON_SIZE} weight="bold" />}
            onPress={togglePicker('place')}
            testID="ask-place-chip"
          />
        </View>

        {openPicker === 'language' ? (
          <View style={styles.picker} testID="ask-language-picker">
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
          <View style={styles.picker} testID="ask-place-picker">
            <LocationPicker
              text={locationText}
              selected={selectedPlace}
              onChangeText={(next) => {
                setLocationText(next);
                // Typing after a selection invalidates it until a new one is chosen.
                setSelectedPlace(null);
              }}
              onSelect={(chosen) => {
                setSelectedPlace(chosen);
                setLocationText(chosen.place);
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

        {/* Borderless fields: no label, no underline, no box. The type scale is
            the only hierarchy — the question at `h1`, the detail at `reading` — so
            the page reads as one piece of writing rather than as a form
            (KNOT-020k-fix14). */}
        <TextInput
          style={[typeScale.h1, styles.title]}
          value={title}
          onChangeText={setTitle}
          placeholder={TITLE_PLACEHOLDER}
          placeholderTextColor={colors.text.disabled}
          accessibilityLabel="Your question"
          multiline
          testID="ask-title"
        />
        <TextInput
          style={[typeScale.reading, styles.body]}
          value={body}
          onChangeText={setBody}
          placeholder={BODY_PLACEHOLDER}
          placeholderTextColor={colors.text.disabled}
          accessibilityLabel="Add context so people can answer well"
          multiline
          textAlignVertical="top"
          testID="ask-body"
        />

        {error !== undefined ? <Text style={[typeScale.bodyS, styles.error]}>{error}</Text> : null}
      </ScrollView>

      {/* The footer says who asking will reach, before anything is asked. The glyph
          is a mark, not a control: an inquiry carries no media — the API accepts
          text, a language and a place only (KNOT-020k-fix14). */}
      <View style={styles.footer} testID="ask-footer">
        <View accessibilityElementsHidden importantForAccessibility="no">
          <ImageSquare color={colors.text.tertiary} size={FOOTER_ICON_SIZE} weight="bold" />
        </View>
        <Text style={[typeScale.caption, styles.footerNote]} numberOfLines={2}>
          {audienceNote(place)}
        </Text>
      </View>
    </KeyboardAvoidingView>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    // The detail field: tall enough that the page looks like a page, with the text
    // starting at the top rather than centred.
    body: {
      color: colors.text.primary,
      marginTop: spacing.lg,
      minHeight: 140,
      paddingVertical: 0,
    },
    // The metadata row sits under the bar, above the writing.
    chips: {
      flexDirection: 'row',
      flexWrap: 'wrap',
      gap: spacing.sm,
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
      marginTop: spacing.lg,
    },
    footer: {
      alignItems: 'center',
      borderTopColor: colors.border.hairline,
      borderTopWidth: 1,
      flexDirection: 'row',
      gap: spacing.sm,
      paddingHorizontal: layout.screenPaddingReading,
      paddingVertical: spacing.md,
    },
    footerNote: {
      color: colors.text.tertiary,
      flexShrink: 1,
    },
    // A 44-point tap target for the bar's close control.
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    // A revealed picker keeps its own look; it only needs air around it.
    picker: {
      marginTop: spacing.md,
    },
    // The question itself, at the largest step: the page's headline is the text a
    // person writes, so it takes the same weight a title does elsewhere.
    title: {
      color: colors.text.primary,
      marginTop: spacing.xl,
      paddingVertical: 0,
    },
  });

import React, { useState } from 'react';
import {
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { X } from 'phosphor-react-native';

import { describeError } from '../../api/client';
import { inquiriesApi } from '../../api/inquiries';
import Button from '../../components/Button';
import Header from '../../components/Header';
import Input from '../../components/Input';
import LanguagePicker from '../../components/LanguagePicker';
import LocationPicker, { PickedPlace } from '../../components/LocationPicker';
import { layout, spacing, typeScale, useTheme, useThemedStyles } from '../../theme';
import type { ThemeColors } from '../../theme';
import { inquiryAudienceLabel, inquiryDraftError } from '../../utils/inquiry';

type AskInquiryScreenProps = {
  /** The signed-in user's access token. */
  readonly token: string;
  /** The language to start with, from the user's preferences. */
  readonly defaultLanguage: string;
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
 * The place field is what makes an inquiry different from a story: naming a place
 * routes the question to the first Rooted people there, and the form says so
 * before anything is sent, so asking is never a surprise. Leaving it blank is
 * allowed — the question is still public, it is simply not announced to anyone.
 *
 * The asker is taken from the access token by the server; there is no author field
 * and no anonymous option, because every inquiry is attributed (KNOT-ADR-055).
 */
export default function AskInquiryScreen({
  token,
  defaultLanguage,
  onAsked,
  onCancel,
}: AskInquiryScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [language, setLanguage] = useState(defaultLanguage);
  const [locationText, setLocationText] = useState('');
  const [selectedPlace, setSelectedPlace] = useState<PickedPlace | null>(null);

  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  // The place that will actually be sent. A selection wins over the typed text,
  // because only a selection carries the coordinate the geocoder resolved.
  const place = selectedPlace?.place ?? locationText.trim();

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
      <Header
        title="Ask a question"
        left={
          <Pressable
            style={styles.iconButton}
            onPress={onCancel}
            accessibilityRole="button"
            accessibilityLabel="Close"
          >
            <X color={colors.text.primary} size={24} />
          </Pressable>
        }
      />

      <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        <Text style={[typeScale.bodyS, styles.subtitle]}>
          Ask the people who know a place something only they would know. Your question is public
          and carries your name.
        </Text>

        <View style={styles.field}>
          <Input
            label="Question"
            value={title}
            onChangeText={setTitle}
            accessibilityLabel="Your question"
            placeholder="Why do the cattle come home at the same hour?"
          />
        </View>

        <View style={styles.field}>
          <Input
            label="Detail"
            value={body}
            onChangeText={setBody}
            multiline
            accessibilityLabel="Add context so people can answer well"
            placeholder="Say more about what you want to know"
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

        <Text style={[typeScale.overline, styles.label]}>Place (optional)</Text>
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
          }}
          onClear={() => {
            setLocationText('');
            setSelectedPlace(null);
          }}
          placeholder="Search for a place"
        />

        <Text style={[typeScale.caption, styles.audience]}>{inquiryAudienceLabel(place)}</Text>

        {error !== undefined ? <Text style={[typeScale.bodyS, styles.error]}>{error}</Text> : null}

        <View style={styles.submit}>
          <Button
            variant="primary"
            fullWidth
            label="Ask"
            loading={submitting}
            accessibilityLabel="Ask your question"
            onPress={() => {
              void handleSubmit();
            }}
          />
        </View>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    // The routing note, in the quiet tertiary ink, beneath the place field.
    audience: {
      color: colors.text.tertiary,
      marginTop: spacing.md,
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
    field: {
      marginTop: spacing.lg,
    },
    // A 44-point tap target for the header's close button.
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    label: {
      color: colors.text.secondary,
      marginBottom: spacing.sm,
      marginTop: spacing.xl,
    },
    subtitle: {
      color: colors.text.secondary,
    },
    submit: {
      marginTop: spacing.xl,
    },
  });

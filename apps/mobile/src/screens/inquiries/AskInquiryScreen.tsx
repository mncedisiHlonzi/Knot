import React, { useState } from 'react';
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import { describeError } from '../../api/client';
import { inquiriesApi } from '../../api/inquiries';
import LanguagePicker from '../../components/LanguagePicker';
import LocationPicker, { PickedPlace } from '../../components/LocationPicker';
import { colors, fontSizes, fontWeights, lineHeights, radius, spacing } from '../../theme';
import {
  INQUIRY_BODY_MAX_LENGTH,
  INQUIRY_TITLE_MAX_LENGTH,
  inquiryAudienceLabel,
  inquiryDraftError,
} from '../../utils/inquiry';

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
      <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        <Pressable style={styles.link} onPress={onCancel}>
          <Text style={styles.linkText}>← Cancel</Text>
        </Pressable>

        <Text style={styles.title}>Ask a question</Text>
        <Text style={styles.subtitle}>
          Ask the people who know a place something only they would know. Your question is public
          and carries your name.
        </Text>

        <Text style={styles.label}>Question</Text>
        <TextInput
          style={styles.input}
          value={title}
          onChangeText={setTitle}
          maxLength={INQUIRY_TITLE_MAX_LENGTH}
          placeholder="Why do the cattle come home at the same hour?"
          placeholderTextColor={colors.text.secondary}
        />

        <Text style={styles.label}>Detail</Text>
        <TextInput
          style={[styles.input, styles.multiline]}
          value={body}
          onChangeText={setBody}
          maxLength={INQUIRY_BODY_MAX_LENGTH}
          multiline
          placeholder="Say more about what you want to know"
          placeholderTextColor={colors.text.secondary}
        />

        <Text style={styles.label}>Language</Text>
        <LanguagePicker
          mode="single"
          selected={language}
          onSelect={(chosen) => setLanguage(chosen.code)}
          onClear={() => setLanguage('')}
          placeholder="Search, e.g. Zulu or zul"
        />

        <Text style={styles.label}>Place (optional)</Text>
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

        <View style={styles.audience}>
          <Text style={styles.audienceText}>{inquiryAudienceLabel(place)}</Text>
        </View>

        {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

        <Pressable
          style={[styles.submit, submitting ? styles.submitDisabled : null]}
          onPress={() => {
            void handleSubmit();
          }}
          disabled={submitting}
          accessibilityRole="button"
          accessibilityLabel="Ask your question"
        >
          {submitting ? (
            <ActivityIndicator color={colors.text.primary} />
          ) : (
            <Text style={styles.submitText}>Ask</Text>
          )}
        </Pressable>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  audience: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.subtle,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.lg,
    padding: spacing.md,
  },
  audienceText: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    lineHeight: lineHeights.sm,
  },
  container: {
    backgroundColor: colors.bg.primary,
    flex: 1,
  },
  content: {
    padding: spacing.lg,
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.sm,
    marginTop: spacing.md,
  },
  input: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    color: colors.text.primary,
    fontSize: fontSizes.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  label: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.medium,
    marginBottom: spacing.xs,
    marginTop: spacing.lg,
  },
  link: {
    marginBottom: spacing.md,
  },
  linkText: {
    color: colors.text.brand,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.medium,
  },
  multiline: {
    minHeight: 120,
    textAlignVertical: 'top',
  },
  submit: {
    alignItems: 'center',
    backgroundColor: colors.brand.purple,
    borderRadius: radius.md,
    marginTop: spacing.xl,
    paddingVertical: spacing.md,
  },
  submitDisabled: {
    opacity: 0.6,
  },
  submitText: {
    color: colors.text.primary,
    fontSize: fontSizes.md,
    fontWeight: fontWeights.semiBold,
  },
  subtitle: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    lineHeight: lineHeights.sm,
    marginTop: spacing.xs,
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes['2xl'],
    fontWeight: fontWeights.bold,
  },
});

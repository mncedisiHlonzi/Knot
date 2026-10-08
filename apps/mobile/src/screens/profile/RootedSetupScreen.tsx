import React, { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Switch, Text, TextInput, View } from 'react-native';

import { describeError } from '../../api/client';
import { DURATION_BUCKETS, DurationBucket, rootedApi } from '../../api/rooted';
import { colors, fontSizes, fontWeights, radius, spacing } from '../../theme';

type RootedSetupScreenProps = {
  /** The signed-in user's access token. The server derives the owner from it. */
  readonly token: string;
  /** Called once the signal has been stored. */
  readonly onSaved: () => void;
  /** Called when the person abandons the form. */
  readonly onCancel: () => void;
};

/** The longest place the server accepts, mirroring its rule. */
const MAX_PLACE_LENGTH = 80;

/** Display labels for the duration buckets, in the order the form offers them. */
const DURATION_LABELS: Readonly<Record<DurationBucket, string>> = {
  lifelong: 'Lifelong',
  many_years: 'Many years',
  several_years: 'Several years',
  a_few_years: 'A few years',
  recently: 'Recently',
};

/**
 * Returns the first client-side validation problem, or undefined when the form is
 * acceptable. The server validates again and remains the source of truth.
 */
function validateForm(place: string, bucket: DurationBucket | null): string | undefined {
  if (place.trim() === '') {
    return 'A place is required.';
  }
  if (place.trim().length > MAX_PLACE_LENGTH) {
    return `The place must be at most ${MAX_PLACE_LENGTH} characters.`;
  }
  if (bucket === null) {
    return 'Choose how long you have been rooted there.';
  }
  return undefined;
}

/**
 * The Rooted setup form: where you are rooted, for how long, and whether it is
 * public.
 *
 * Rooted is deliberately honest and simple: a place at city or region precision, a
 * duration bucket, and a visibility flag. There is no verification, no vouching,
 * and no score. A person has one active signal, so submitting replaces any
 * existing one.
 */
export default function RootedSetupScreen({
  token,
  onSaved,
  onCancel,
}: RootedSetupScreenProps): React.ReactElement {
  const [place, setPlace] = useState('');
  const [bucket, setBucket] = useState<DurationBucket | null>(null);
  const [isPublic, setIsPublic] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(): Promise<void> {
    const problem = validateForm(place, bucket);
    if (problem !== undefined) {
      setError(problem);
      return;
    }

    setError(undefined);
    setSubmitting(true);

    try {
      await rootedApi.setMySignal(token, {
        place: place.trim(),
        duration_bucket: bucket as DurationBucket,
        is_public: isPublic,
      });
      onSaved();
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
      <Pressable style={styles.link} onPress={onCancel}>
        <Text style={styles.linkText}>← Back</Text>
      </Pressable>

      <Text style={styles.title}>Where are you rooted?</Text>
      <Text style={styles.hint}>
        A city or a region, not a street address. Rooted is self-declared and never verified.
      </Text>

      <Text style={styles.label}>Place</Text>
      <TextInput
        style={styles.input}
        value={place}
        onChangeText={setPlace}
        maxLength={MAX_PLACE_LENGTH}
        placeholder="Cape Town"
        placeholderTextColor={colors.text.secondary}
      />

      <Text style={styles.label}>How long have you been rooted there?</Text>
      <View style={styles.buckets}>
        {DURATION_BUCKETS.map((option) => (
          <Pressable
            key={option}
            style={[styles.bucket, bucket === option ? styles.bucketSelected : null]}
            onPress={() => setBucket(option)}
          >
            <Text style={bucket === option ? styles.bucketTextSelected : styles.bucketText}>
              {DURATION_LABELS[option]}
            </Text>
          </Pressable>
        ))}
      </View>

      <View style={styles.switchRow}>
        <Text style={styles.label}>Show publicly</Text>
        <Switch value={isPublic} onValueChange={setIsPublic} />
      </View>
      <Text style={styles.hint}>
        Rooted signals are public by default. Turn this off to keep yours to yourself; nobody else
        will see it.
      </Text>

      {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

      <Pressable
        style={[styles.button, submitting ? styles.buttonDisabled : null]}
        onPress={handleSubmit}
        disabled={submitting}
      >
        <Text style={styles.buttonText}>{submitting ? 'Saving…' : 'Save my Rooted signal'}</Text>
      </Pressable>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  bucket: {
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    marginRight: spacing.md,
    marginTop: spacing.sm,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
  },
  bucketSelected: {
    backgroundColor: colors.brand.purple,
    borderColor: colors.brand.purple,
  },
  bucketText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  bucketTextSelected: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  buckets: {
    flexDirection: 'row',
    flexWrap: 'wrap',
  },
  button: {
    alignItems: 'center',
    backgroundColor: colors.brand.purple,
    borderRadius: radius.md,
    marginTop: spacing.xl,
    paddingVertical: spacing.md,
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
    marginTop: spacing.sm,
  },
  input: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    color: colors.text.primary,
    fontSize: fontSizes.md,
    marginTop: spacing.sm,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
  },
  label: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
    marginTop: spacing.xl,
  },
  link: {
    marginBottom: spacing.md,
  },
  linkText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
  },
  switchRow: {
    alignItems: 'center',
    flexDirection: 'row',
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
  },
});

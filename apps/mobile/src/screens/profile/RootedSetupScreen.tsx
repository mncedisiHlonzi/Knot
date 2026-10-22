import React, { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Switch, Text, View } from 'react-native';

import { describeError } from '../../api/client';
import { DURATION_BUCKETS, DurationBucket, rootedApi } from '../../api/rooted';
import Button from '../../components/Button';
import Chip from '../../components/Chip';
import LocationPicker, { PickedPlace } from '../../components/LocationPicker';
import { spacing, typeScale, useTheme, useThemedStyles } from '../../theme';
import type { ThemeColors } from '../../theme';

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
function validateForm(
  placeText: string,
  bucket: DurationBucket | null,
  selectedPlace: PickedPlace | null,
): string | undefined {
  if (placeText.trim() === '') {
    return 'A place is required.';
  }
  if (placeText.trim().length > MAX_PLACE_LENGTH) {
    return `The place must be at most ${MAX_PLACE_LENGTH} characters.`;
  }
  if (selectedPlace === null) {
    return 'Please select a place from the suggestions.';
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
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [placeText, setPlaceText] = useState('');
  const [selectedPlace, setSelectedPlace] = useState<PickedPlace | null>(null);
  const [bucket, setBucket] = useState<DurationBucket | null>(null);
  const [isPublic, setIsPublic] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(): Promise<void> {
    const problem = validateForm(placeText, bucket, selectedPlace);
    if (problem !== undefined || selectedPlace === null) {
      setError(problem ?? 'Please select a place from the suggestions.');
      return;
    }

    setError(undefined);
    setSubmitting(true);

    try {
      await rootedApi.setMySignal(token, {
        place: selectedPlace.place,
        duration_bucket: bucket as DurationBucket,
        is_public: isPublic,
        latitude: selectedPlace.latitude,
        longitude: selectedPlace.longitude,
        ...(selectedPlace.country === null ? {} : { place_country: selectedPlace.country }),
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
      {/* A plain text link rather than a header control: this screen is reached
          from the profile's options sheet, and the brief for this task migrates
          the three screens' styling without moving their layout
          (KNOT-020g-fix14). The tap target still meets the 44-point minimum. */}
      <Pressable
        style={styles.link}
        onPress={onCancel}
        accessibilityRole="button"
        accessibilityLabel="Back"
      >
        <Text style={[typeScale.button, styles.linkText]}>← Back</Text>
      </Pressable>

      <Text style={[typeScale.h1, styles.title]}>Where are you rooted?</Text>
      <Text style={[typeScale.bodyS, styles.hint]}>
        A city or a region, not a street address. Rooted is self-declared and never verified.
      </Text>

      <Text style={[typeScale.overline, styles.label]}>Place</Text>
      <LocationPicker
        text={placeText}
        selected={selectedPlace}
        onChangeText={(next) => {
          setPlaceText(next);
          setSelectedPlace(null);
        }}
        onSelect={(place) => {
          setSelectedPlace(place);
          setPlaceText(place.place);
        }}
        onClear={() => {
          setPlaceText('');
          setSelectedPlace(null);
        }}
        placeholder="Search for a city or region"
      />

      <Text style={[typeScale.overline, styles.label]}>How long have you been rooted there?</Text>
      {/* The duration buckets are the design system's `Chip`, the same control the
          Discovery screens filter with: a single choice, marked by the brand tint
          and the brand ink (KNOT-020g-fix14). */}
      <View style={styles.buckets}>
        {DURATION_BUCKETS.map((option) => (
          <Chip
            key={option}
            label={DURATION_LABELS[option]}
            selected={bucket === option}
            onPress={() => setBucket(option)}
          />
        ))}
      </View>

      <View style={styles.switchRow}>
        <Text style={[typeScale.overline, styles.label]}>Show publicly</Text>
        <Switch
          value={isPublic}
          onValueChange={setIsPublic}
          trackColor={{ false: colors.bg.sunken, true: colors.brand.primary }}
        />
      </View>
      <Text style={[typeScale.bodyS, styles.hint]}>
        Rooted signals are public by default. Turn this off to keep yours to yourself; nobody else
        will see it.
      </Text>

      {error !== undefined ? <Text style={[typeScale.body, styles.error]}>{error}</Text> : null}

      {/* The submit action is the design system's primary `Button`; its `loading`
          state is what the hand-rolled "Saving…" label used to be
          (KNOT-020g-fix14). */}
      <Button
        variant="primary"
        fullWidth
        label="Save my Rooted signal"
        loading={submitting}
        onPress={() => {
          void handleSubmit();
        }}
      />
    </ScrollView>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    buckets: {
      flexDirection: 'row',
      flexWrap: 'wrap',
      gap: spacing.sm,
    },
    content: {
      backgroundColor: colors.bg.canvas,
      gap: spacing.lg,
      padding: spacing.xl,
      paddingBottom: spacing.xxxl,
    },
    error: {
      color: colors.state.danger,
    },
    hint: {
      color: colors.text.secondary,
    },
    label: {
      color: colors.text.secondary,
    },
    // The back link keeps a 44-point tap target even though it is only as tall as
    // its label.
    link: {
      justifyContent: 'center',
      minHeight: 44,
    },
    linkText: {
      color: colors.brand.primary,
    },
    // The row is left exactly as it was — the label then the switch — so this task
    // changes the screen's tokens and its controls, not its layout
    // (KNOT-020g-fix14).
    switchRow: {
      alignItems: 'center',
      flexDirection: 'row',
    },
    title: {
      color: colors.text.primary,
    },
  });

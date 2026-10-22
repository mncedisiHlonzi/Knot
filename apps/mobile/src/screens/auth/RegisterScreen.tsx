import React, { useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';

import { api, AuthResponse, describeError } from '../../api/client';
import Button from '../../components/Button';
import Header from '../../components/Header';
import Input from '../../components/Input';
import LanguagePicker from '../../components/LanguagePicker';
import Logomark from '../../components/Logomark';
import { layout, spacing, typeScale, useThemedStyles, type ThemeColors } from '../../theme';

/** The logomark's size in the auth header, one step up from the Feed's 32. */
const LOGOMARK_SIZE = 36;

/**
 * The mark's horizontal nudge, so it lines up with the form's 24pt column.
 *
 * The header's own gutter is `layout.screenPaddingFeed` (16pt); the auth content
 * sits at `layout.screenPaddingReading` (24pt). The extra 8pt lifts the mark onto
 * that column (KNOT-020g-fix13, KNOT-020j-fix2).
 */
const LOGOMARK_INSET = spacing.sm;

type RegisterScreenProps = {
  /** Called with the issued tokens and user once registration succeeds. */
  readonly onAuthenticated: (result: AuthResponse) => void;
  /** Called when the person would rather sign in to an existing account. */
  readonly onSwitchToLogin: () => void;
};

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const MIN_PASSWORD_LENGTH = 8;
const MAX_DISPLAY_NAME_LENGTH = 80;
const MAX_LOCATION_LENGTH = 120;
const MAX_PHONE_LENGTH = 32;

/**
 * Returns the first client-side validation problem, or undefined when the form is
 * acceptable. The server validates again and remains the source of truth.
 */
function validateForm(email: string, password: string, displayName: string): string | undefined {
  if (email.trim() === '') {
    return 'Email is required.';
  }
  if (!EMAIL_PATTERN.test(email.trim())) {
    return 'Email must look like name@example.com.';
  }
  if (password.length < MIN_PASSWORD_LENGTH) {
    return `Password must be at least ${MIN_PASSWORD_LENGTH} characters.`;
  }
  if (displayName.trim() === '') {
    return 'Display name is required.';
  }
  if (displayName.trim().length > MAX_DISPLAY_NAME_LENGTH) {
    return `Display name must be at most ${MAX_DISPLAY_NAME_LENGTH} characters.`;
  }
  return undefined;
}

/**
 * Registration form. Tokens are handed to the caller and kept in memory only.
 *
 * It shares the Login screen's editorial frame (KNOT-020j): the logomark in the
 * header, the display headline and one sentence at the top, then the form in a
 * single 24pt column. The optional fields stay in the same list rather than
 * moving behind a disclosure — the page should read as one continuous form, the
 * way the register reference draws it.
 */
export default function RegisterScreen({
  onAuthenticated,
  onSwitchToLogin,
}: RegisterScreenProps): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [languages, setLanguages] = useState<readonly string[]>([]);
  const [location, setLocation] = useState('');
  const [phone, setPhone] = useState('');
  const [error, setError] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(): Promise<void> {
    const problem = validateForm(email, password, displayName);
    if (problem !== undefined) {
      setError(problem);
      return;
    }

    setError(undefined);
    setSubmitting(true);

    try {
      const preferredLanguages = languages;
      const result = await api.register({
        email: email.trim(),
        password,
        display_name: displayName.trim(),
        ...(preferredLanguages.length > 0 ? { preferred_languages: preferredLanguages } : {}),
        ...(location.trim() === '' ? {} : { approximate_location: location.trim() }),
        ...(phone.trim() === '' ? {} : { phone: phone.trim() }),
      });
      onAuthenticated(result);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <View style={styles.screen}>
      {/* The mark replaces a title in the bar, exactly as the Login screen draws
          it, so the two auth pages feel like two pages of one book. */}
      <Header
        left={
          <View style={styles.logomark}>
            <Logomark size={LOGOMARK_SIZE} />
          </View>
        }
      />

      <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        <View style={styles.intro}>
          <Text style={[typeScale.h1, styles.title]}>Create your account</Text>
          <Text style={[typeScale.body, styles.subtitle]}>
            A place for the stories you carry, in every language you speak.
          </Text>
        </View>

        {/* The fields are the design system's underline `Input`, the same control
            every other form in the app uses (KNOT-020g-fix14). Its label is drawn
            in the overline style, so the one standalone label below — the language
            picker's, which is not an `Input` — matches it. */}
        <View style={styles.form}>
          <Input
            label="Email"
            value={email}
            onChangeText={setEmail}
            autoCapitalize="none"
            autoCorrect={false}
            keyboardType="email-address"
            placeholder="you@example.com"
          />

          <Input
            label="Password"
            value={password}
            onChangeText={setPassword}
            secureTextEntry
            placeholder={`At least ${MIN_PASSWORD_LENGTH} characters`}
          />

          <Input
            label="Display name"
            value={displayName}
            onChangeText={setDisplayName}
            placeholder="How you want to be known"
          />

          <View>
            <Text style={[typeScale.overline, styles.fieldLabel]}>Languages you speak</Text>
            <LanguagePicker
              mode="multiple"
              selected={languages}
              onChange={setLanguages}
              placeholder="Search, e.g. Zulu or zul"
            />
          </View>

          {/* `Input` has no `maxLength` prop, and extending the design system's
              field is out of scope for this task, so the same cap is applied in the
              change handler instead: the value handed to state is never longer than
              the server's limit, which is what `maxLength` enforced
              (KNOT-020g-fix14). */}
          <Input
            label="Approximate location (optional)"
            value={location}
            onChangeText={(next) => setLocation(next.slice(0, MAX_LOCATION_LENGTH))}
            placeholder="Cape Town"
          />

          <Input
            label="Phone (optional)"
            value={phone}
            onChangeText={(next) => setPhone(next.slice(0, MAX_PHONE_LENGTH))}
            keyboardType="phone-pad"
            placeholder="+27 00 000 0000"
          />

          {error !== undefined ? <Text style={[typeScale.body, styles.error]}>{error}</Text> : null}
        </View>

        <View style={styles.actions}>
          {/* The submit action is the design system's primary `Button`; its
              `loading` state is what the hand-rolled "Creating account…" label
              used to be (KNOT-020g-fix14). */}
          <Button
            variant="primary"
            fullWidth
            label="Create account"
            loading={submitting}
            onPress={() => {
              void handleSubmit();
            }}
          />

          <Button
            variant="ghost"
            fullWidth
            label="Already have an account? Sign in"
            onPress={onSwitchToLogin}
          />
        </View>
      </ScrollView>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    // The two chrome elements — the buttons — close the page: the gap above them
    // is larger than the gap between fields, so the form reads as signed off.
    actions: {
      gap: spacing.sm,
      marginTop: spacing.xl,
    },
    content: {
      paddingBottom: spacing.xxxl,
      paddingHorizontal: layout.screenPaddingReading,
      paddingTop: spacing.xl,
    },
    error: {
      color: colors.state.danger,
    },
    fieldLabel: {
      color: colors.text.secondary,
      marginBottom: spacing.xs,
    },
    form: {
      gap: spacing.lg,
    },
    // The headline and its sentence are one block of air above the form.
    intro: {
      gap: spacing.sm,
      marginBottom: spacing.xxl,
    },
    logomark: {
      marginLeft: LOGOMARK_INSET,
    },
    screen: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    subtitle: {
      color: colors.text.secondary,
    },
    title: {
      color: colors.text.primary,
    },
  });

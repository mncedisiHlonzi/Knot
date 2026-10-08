import React, { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';

import { api, AuthResponse, describeError } from '../../api/client';
import { colors, fontSizes, fontWeights, radius, spacing } from '../../theme';

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
 * Splits the comma-separated language input into trimmed, non-empty entries.
 *
 * A full multi-select is a later task; for now the server still receives a proper
 * array rather than a raw string.
 */
function parseLanguages(value: string): string[] {
  return value
    .split(',')
    .map((entry) => entry.trim())
    .filter((entry) => entry !== '');
}

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
 */
export default function RegisterScreen({
  onAuthenticated,
  onSwitchToLogin,
}: RegisterScreenProps): React.ReactElement {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [languages, setLanguages] = useState('');
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
      const preferredLanguages = parseLanguages(languages);
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
    <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
      <Text style={styles.title}>Create your Knot account</Text>

      <Text style={styles.label}>Email</Text>
      <TextInput
        style={styles.input}
        value={email}
        onChangeText={setEmail}
        autoCapitalize="none"
        autoCorrect={false}
        keyboardType="email-address"
        placeholder="you@example.com"
        placeholderTextColor={colors.text.secondary}
      />

      <Text style={styles.label}>Password</Text>
      <TextInput
        style={styles.input}
        value={password}
        onChangeText={setPassword}
        secureTextEntry
        placeholder={`At least ${MIN_PASSWORD_LENGTH} characters`}
        placeholderTextColor={colors.text.secondary}
      />

      <Text style={styles.label}>Display name</Text>
      <TextInput
        style={styles.input}
        value={displayName}
        onChangeText={setDisplayName}
        placeholder="How you want to be known"
        placeholderTextColor={colors.text.secondary}
      />

      <Text style={styles.label}>Languages you speak (comma separated)</Text>
      <TextInput
        style={styles.input}
        value={languages}
        onChangeText={setLanguages}
        autoCapitalize="none"
        placeholder="en, zu, fr"
        placeholderTextColor={colors.text.secondary}
      />

      <Text style={styles.label}>Approximate location (optional)</Text>
      <TextInput
        style={styles.input}
        value={location}
        onChangeText={setLocation}
        maxLength={MAX_LOCATION_LENGTH}
        placeholder="Cape Town"
        placeholderTextColor={colors.text.secondary}
      />

      <Text style={styles.label}>Phone (optional)</Text>
      <TextInput
        style={styles.input}
        value={phone}
        onChangeText={setPhone}
        maxLength={MAX_PHONE_LENGTH}
        keyboardType="phone-pad"
        placeholder="+27 00 000 0000"
        placeholderTextColor={colors.text.secondary}
      />

      {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

      <Pressable
        style={[styles.button, submitting ? styles.buttonDisabled : null]}
        onPress={handleSubmit}
        disabled={submitting}
      >
        <Text style={styles.buttonText}>{submitting ? 'Creating account…' : 'Create account'}</Text>
      </Pressable>

      <Pressable style={styles.link} onPress={onSwitchToLogin}>
        <Text style={styles.linkText}>Already have an account? Sign in</Text>
      </Pressable>

      <View style={styles.spacer} />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  button: {
    alignItems: 'center',
    backgroundColor: colors.brand.purple,
    borderRadius: radius.md,
    marginTop: 20,
    paddingVertical: 14,
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  buttonText: {
    color: colors.text.inverse,
    fontSize: fontSizes.md,
    fontWeight: fontWeights.semiBold,
  },
  content: {
    padding: spacing.xl,
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.base,
    marginTop: spacing.lg,
  },
  input: {
    backgroundColor: colors.bg.inverse,
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    fontSize: fontSizes.md,
    paddingHorizontal: spacing.md,
    paddingVertical: 10,
  },
  label: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
    marginBottom: 6,
    marginTop: spacing.lg,
  },
  link: {
    alignItems: 'center',
    marginTop: 18,
  },
  linkText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
  },
  spacer: {
    height: spacing['2xl'],
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
    marginBottom: spacing.sm,
  },
});

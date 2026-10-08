import React, { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, TextInput } from 'react-native';

import { api, AuthResponse, describeError } from '../../api/client';
import { colors, fontSizes, fontWeights, radius, spacing } from '../../theme';

type LoginScreenProps = {
  /** Called with the issued tokens and user once login succeeds. */
  readonly onAuthenticated: (result: AuthResponse) => void;
  /** Called when the person needs an account instead. */
  readonly onSwitchToRegister: () => void;
};

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/**
 * Returns the first client-side validation problem, or undefined when the form is
 * acceptable. The server validates again and remains the source of truth.
 */
function validateForm(email: string, password: string): string | undefined {
  if (email.trim() === '') {
    return 'Email is required.';
  }
  if (!EMAIL_PATTERN.test(email.trim())) {
    return 'Email must look like name@example.com.';
  }
  if (password === '') {
    return 'Password is required.';
  }
  return undefined;
}

/**
 * Login form. Tokens are handed to the caller and kept in memory only.
 */
export default function LoginScreen({
  onAuthenticated,
  onSwitchToRegister,
}: LoginScreenProps): React.ReactElement {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(): Promise<void> {
    const problem = validateForm(email, password);
    if (problem !== undefined) {
      setError(problem);
      return;
    }

    setError(undefined);
    setSubmitting(true);

    try {
      const result = await api.login({ email: email.trim(), password });
      onAuthenticated(result);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
      <Text style={styles.title}>Sign in to Knot</Text>

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
        placeholder="Your password"
        placeholderTextColor={colors.text.secondary}
      />

      {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

      <Pressable
        style={[styles.button, submitting ? styles.buttonDisabled : null]}
        onPress={handleSubmit}
        disabled={submitting}
      >
        <Text style={styles.buttonText}>{submitting ? 'Signing in…' : 'Sign in'}</Text>
      </Pressable>

      <Pressable style={styles.link} onPress={onSwitchToRegister}>
        <Text style={styles.linkText}>Need an account? Register</Text>
      </Pressable>
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
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
    marginBottom: spacing.sm,
  },
});

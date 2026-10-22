import React, { useState } from 'react';
import { ScrollView, StyleSheet, Text } from 'react-native';

import { api, AuthResponse, describeError } from '../../api/client';
import Button from '../../components/Button';
import Input from '../../components/Input';
import { spacing, typeScale, useThemedStyles, type ThemeColors } from '../../theme';

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
  const styles = useThemedStyles(createStyles);
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
      <Text style={[typeScale.h1, styles.title]}>Sign in to Knot</Text>

      {/* The fields are the design system's underline `Input`, the same control
          every other form in the app uses, so the auth screens read as the rest of
          Knot rather than as a bespoke boxed form (KNOT-020g-fix14). The label is
          the field's own, drawn in the overline style. */}
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
        placeholder="Your password"
      />

      {error !== undefined ? <Text style={[typeScale.body, styles.error]}>{error}</Text> : null}

      {/* The submit action is the design system's primary `Button`; its `loading`
          state is what the hand-rolled "Signing in…" label used to be — the action
          is disabled and busy, and a spinner replaces the label
          (KNOT-020g-fix14). */}
      <Button
        variant="primary"
        fullWidth
        label="Sign in"
        loading={submitting}
        onPress={() => {
          void handleSubmit();
        }}
      />

      <Button
        variant="ghost"
        fullWidth
        label="Don't have an account? Create one"
        onPress={onSwitchToRegister}
      />
    </ScrollView>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    content: {
      backgroundColor: colors.bg.canvas,
      gap: spacing.lg,
      padding: spacing.xl,
    },
    error: {
      color: colors.state.danger,
    },
    title: {
      color: colors.text.primary,
    },
  });

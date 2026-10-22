import React, { useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';

import { api, AuthResponse, describeError } from '../../api/client';
import Button from '../../components/Button';
import Header from '../../components/Header';
import Input from '../../components/Input';
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
 *
 * The screen is composed as the opening page of a book (KNOT-020j): the logomark
 * stands in the header where a running head would be, the headline is the
 * display face at `typeScale.h1`, a single sentence sets the tone beneath it, and
 * the form follows with generous air. Nothing is boxed or shaded — the page is
 * the canvas, and the only strong element is the one filled action.
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
    <View style={styles.screen}>
      {/* The mark replaces a title in the bar: the page below already says what
          this screen is, so the header stays quiet. */}
      <Header
        left={
          <View style={styles.logomark}>
            <Logomark size={LOGOMARK_SIZE} />
          </View>
        }
      />

      <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        <View style={styles.intro}>
          <Text style={[typeScale.h1, styles.title]}>Welcome back</Text>
          <Text style={[typeScale.body, styles.subtitle]}>
            Sign in to pick up the stories you were reading, and the ones you were telling.
          </Text>
        </View>

        {/* The fields are the design system's underline `Input`, the same control
            every other form in the app uses, so the auth screens read as the rest of
            Knot rather than as a bespoke boxed form (KNOT-020g-fix14). */}
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
            placeholder="Your password"
          />

          {error !== undefined ? <Text style={[typeScale.body, styles.error]}>{error}</Text> : null}
        </View>

        <View style={styles.actions}>
          {/* The submit action is the design system's primary `Button`; its
              `loading` state is what the hand-rolled "Signing in…" label used to
              be (KNOT-020g-fix14). */}
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
            label="New to Knot? Create an account"
            onPress={onSwitchToRegister}
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
      marginTop: spacing.lg,
    },
    content: {
      paddingBottom: spacing.xxxl,
      paddingHorizontal: layout.screenPaddingReading,
      paddingTop: spacing.xl,
    },
    error: {
      color: colors.state.danger,
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

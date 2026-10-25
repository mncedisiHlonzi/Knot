import React, { useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';

import { resetPassword } from '../../api/auth';
import { describeError } from '../../api/client';
import Button from '../../components/Button';
import Header from '../../components/Header';
import Input from '../../components/Input';
import Logomark from '../../components/Logomark';
import { layout, spacing, typeScale, useThemedStyles, type ThemeColors } from '../../theme';
import { MIN_PASSWORD_LENGTH, validatePassword } from '../../utils/authForm';

/** The logomark's size in the auth header, matching Login and Register. */
const LOGOMARK_SIZE = 36;

/** The mark's horizontal nudge onto the form's 24pt column (KNOT-020j-fix2). */
const LOGOMARK_INSET = spacing.sm;

type ResetPasswordScreenProps = {
  /** Called when the person returns to the login form. */
  readonly onBackToLogin: () => void;
};

/**
 * Sets a new password from a reset token.
 *
 * The policy is mirrored here as a hint so a mistake costs no round trip, but the
 * server enforces it and remains the truth (KNOT-ADR-086). Success signs every
 * session out — including any the person had on another device — which is stated
 * plainly rather than left as a surprise on the next launch.
 */
export default function ResetPasswordScreen({
  onBackToLogin,
}: ResetPasswordScreenProps): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  const [token, setToken] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);
  const [reset, setReset] = useState(false);

  async function handleSubmit(): Promise<void> {
    const trimmedToken = token.trim();
    if (trimmedToken === '') {
      setError('Paste the token from the reset link.');
      return;
    }

    const problem = validatePassword(password);
    if (problem !== undefined) {
      setError(problem);
      return;
    }

    setError(undefined);
    setSubmitting(true);

    try {
      await resetPassword(trimmedToken, password);
      setReset(true);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <View style={styles.screen}>
      <Header
        left={
          <View style={styles.logomark}>
            <Logomark size={LOGOMARK_SIZE} />
          </View>
        }
      />

      <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        {reset ? (
          <>
            <View style={styles.intro}>
              <Text style={[typeScale.h1, styles.title]}>Password reset.</Text>
              <Text style={[typeScale.body, styles.subtitle]}>
                All sessions have been signed out. Sign in with your new password.
              </Text>
            </View>

            <View style={styles.actions}>
              <Button variant="primary" fullWidth label="Go to login" onPress={onBackToLogin} />
            </View>
          </>
        ) : (
          <>
            <View style={styles.intro}>
              <Text style={[typeScale.h1, styles.title]}>Set a new password</Text>
              <Text style={[typeScale.body, styles.subtitle]}>
                Paste the token from your reset link, then choose a new password.
              </Text>
            </View>

            <View style={styles.form}>
              <Input
                label="Reset token"
                value={token}
                onChangeText={setToken}
                autoCapitalize="none"
                autoCorrect={false}
                placeholder="The long code from the link"
              />

              <Input
                label="New password"
                value={password}
                onChangeText={setPassword}
                secureTextEntry
                placeholder={`At least ${MIN_PASSWORD_LENGTH} characters, with a letter and a number`}
              />

              <Text style={[typeScale.bodyS, styles.note]}>
                Resetting signs you out on every device. Until email delivery is built, the link is
                printed in the server console.
              </Text>

              {error !== undefined ? (
                <Text style={[typeScale.body, styles.error]}>{error}</Text>
              ) : null}
            </View>

            <View style={styles.actions}>
              <Button
                variant="primary"
                fullWidth
                label="Reset password"
                loading={submitting}
                onPress={() => {
                  void handleSubmit();
                }}
              />

              <Button variant="ghost" fullWidth label="Back to login" onPress={onBackToLogin} />
            </View>
          </>
        )}
      </ScrollView>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
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
    intro: {
      gap: spacing.sm,
      marginBottom: spacing.xxl,
    },
    logomark: {
      marginLeft: LOGOMARK_INSET,
    },
    note: {
      color: colors.text.tertiary,
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

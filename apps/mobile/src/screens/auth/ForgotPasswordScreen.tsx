import React, { useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';

import { forgotPassword } from '../../api/auth';
import { describeError } from '../../api/client';
import Button from '../../components/Button';
import Header from '../../components/Header';
import Input from '../../components/Input';
import Logomark from '../../components/Logomark';
import { layout, spacing, typeScale, useThemedStyles, type ThemeColors } from '../../theme';
import { validateEmail } from '../../utils/authForm';

/** The logomark's size in the auth header, matching Login and Register. */
const LOGOMARK_SIZE = 36;

/** The mark's horizontal nudge onto the form's 24pt column (KNOT-020j-fix2). */
const LOGOMARK_INSET = spacing.sm;

type ForgotPasswordScreenProps = {
  /** Called when the person returns to the login form. */
  readonly onBackToLogin: () => void;
  /** Called when the person would rather paste a reset token they already have. */
  readonly onOpenResetPassword: () => void;
};

/**
 * Starts a password reset.
 *
 * The response is the same whether or not the address has an account (KNOT-ADR-087),
 * so the confirmation deliberately says "if that address has an account" — claiming
 * a message was sent would tell an attacker which addresses exist, and would be a
 * lie for the ones that do not.
 */
export default function ForgotPasswordScreen({
  onBackToLogin,
  onOpenResetPassword,
}: ForgotPasswordScreenProps): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  const [email, setEmail] = useState('');
  const [error, setError] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);
  const [sent, setSent] = useState(false);

  async function handleSubmit(): Promise<void> {
    const problem = validateEmail(email);
    if (problem !== undefined) {
      setError(problem);
      return;
    }

    setError(undefined);
    setSubmitting(true);

    try {
      await forgotPassword(email.trim());
      setSent(true);
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
        {sent ? (
          <>
            <View style={styles.intro}>
              <Text style={[typeScale.h1, styles.title]}>Check your email.</Text>
              <Text style={[typeScale.body, styles.subtitle]}>
                If that address has an account, a reset link is on its way. The link expires in 30
                minutes.
              </Text>
            </View>

            <View style={styles.actions}>
              <Button
                variant="primary"
                fullWidth
                label="I have a reset token"
                onPress={onOpenResetPassword}
              />
              <Button variant="ghost" fullWidth label="Back to login" onPress={onBackToLogin} />
            </View>
          </>
        ) : (
          <>
            <View style={styles.intro}>
              <Text style={[typeScale.h1, styles.title]}>Forgot your password?</Text>
              <Text style={[typeScale.body, styles.subtitle]}>
                Enter the address on your account and we will send a link to set a new password.
              </Text>
            </View>

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

              <Text style={[typeScale.bodyS, styles.note]}>
                Until email delivery is built, the link is printed in the server console.
              </Text>

              {error !== undefined ? (
                <Text style={[typeScale.body, styles.error]}>{error}</Text>
              ) : null}
            </View>

            <View style={styles.actions}>
              <Button
                variant="primary"
                fullWidth
                label="Send reset link"
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

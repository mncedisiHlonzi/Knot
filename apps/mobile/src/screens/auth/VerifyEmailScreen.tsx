import React, { useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';

import { verifyEmail } from '../../api/auth';
import { describeError } from '../../api/client';
import Button from '../../components/Button';
import Header from '../../components/Header';
import Input from '../../components/Input';
import Logomark from '../../components/Logomark';
import { layout, spacing, typeScale, useThemedStyles, type ThemeColors } from '../../theme';

/** The logomark's size in the auth header, matching Login and Register. */
const LOGOMARK_SIZE = 36;

/** The mark's horizontal nudge onto the form's 24pt column (KNOT-020j-fix2). */
const LOGOMARK_INSET = spacing.sm;

type VerifyEmailScreenProps = {
  /** Called when the person is done here and wants to sign in. */
  readonly onBackToLogin: () => void;
};

/**
 * Redeems an email-verification token.
 *
 * Until KNOT-031a there is no email provider, so the link is printed in the server
 * console (KNOT-ADR-088) and the token has to be pasted in by hand. The screen is
 * honest about that rather than pretending a message is on its way.
 *
 * The token is the whole credential, so the field is not masked: masking a value
 * that the person has to copy from another window only makes it harder to check
 * that the paste worked.
 */
export default function VerifyEmailScreen({
  onBackToLogin,
}: VerifyEmailScreenProps): React.ReactElement {
  const styles = useThemedStyles(createStyles);
  const [token, setToken] = useState('');
  const [error, setError] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);
  const [verified, setVerified] = useState(false);

  async function handleSubmit(): Promise<void> {
    const trimmed = token.trim();
    if (trimmed === '') {
      setError('Paste the token from the link.');
      return;
    }

    setError(undefined);
    setSubmitting(true);

    try {
      await verifyEmail(trimmed);
      setVerified(true);
    } catch (caught) {
      // The server answers `invalid_verification_token` for unknown, expired and
      // already-used tokens alike. All three mean the same thing to a person: this
      // link is spent, ask for a new one.
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
        {verified ? (
          <>
            <View style={styles.intro}>
              <Text style={[typeScale.h1, styles.title]}>Email verified.</Text>
              <Text style={[typeScale.body, styles.subtitle]}>
                You can now log in. Your account is ready.
              </Text>
            </View>

            <View style={styles.actions}>
              <Button variant="primary" fullWidth label="Go to login" onPress={onBackToLogin} />
            </View>
          </>
        ) : (
          <>
            <View style={styles.intro}>
              <Text style={[typeScale.h1, styles.title]}>Verify your email</Text>
              <Text style={[typeScale.body, styles.subtitle]}>
                Paste the token from your verification link to activate your account.
              </Text>
            </View>

            <View style={styles.form}>
              <Input
                label="Verification token"
                value={token}
                onChangeText={setToken}
                autoCapitalize="none"
                autoCorrect={false}
                placeholder="The long code from the link"
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
                label="Verify"
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

import React, { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { ArrowLeft, CheckSquare, Square } from 'phosphor-react-native';

import { deleteAccount } from '../../api/auth';
import { describeError } from '../../api/client';
import Button from '../../components/Button';
import Header from '../../components/Header';
import Input from '../../components/Input';
import {
  layout,
  spacing,
  typeScale,
  useTheme,
  useThemedStyles,
  type ThemeColors,
} from '../../theme';
import { DELETION_CONSEQUENCES, validateDeleteAccountForm } from '../../utils/deleteAccount';

type DeleteAccountScreenProps = {
  /** The signed-in person's access token. */
  readonly token: string;
  /** Called when the person decides not to delete after all. */
  readonly onBack: () => void;
  /** Called once the account has been deleted, so the app can sign out. */
  readonly onDeleted: () => void;
};

/**
 * Deletes the account, with the two things a destructive action needs: a statement of
 * what will happen, and a deliberate confirmation.
 *
 * The copy is honest about the recovery window rather than frightening (KNOT-ADR-081):
 * the account is not erased the moment the button is pressed, and saying so is the
 * whole point of the window. The confirmation is a checkbox plus the password, because
 * a single mistap should not be able to end an account, and the server refuses the
 * request without the password anyway.
 */
export default function DeleteAccountScreen({
  token,
  onBack,
  onDeleted,
}: DeleteAccountScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [password, setPassword] = useState('');
  const [confirmed, setConfirmed] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);

  async function handleDelete(): Promise<void> {
    const problem = validateDeleteAccountForm(password, confirmed);
    if (problem !== undefined) {
      setError(problem);
      return;
    }

    setError(undefined);
    setSubmitting(true);

    try {
      await deleteAccount(password, token);
      onDeleted();
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <View style={styles.container}>
      <Header
        title="Delete account"
        left={
          <Pressable
            style={styles.iconButton}
            onPress={onBack}
            accessibilityRole="button"
            accessibilityLabel="Back"
          >
            <ArrowLeft color={colors.text.primary} size={24} />
          </Pressable>
        }
      />

      <ScrollView contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        <View style={styles.intro}>
          <Text style={[typeScale.h1, styles.title]}>Delete your account?</Text>
          <Text style={[typeScale.body, styles.subtitle]}>
            This is not immediate and it is not permanent — yet.
          </Text>
        </View>

        <View style={styles.consequences}>
          {DELETION_CONSEQUENCES.map((line) => (
            <Text key={line} style={[typeScale.body, styles.consequence]}>
              {line}
            </Text>
          ))}
        </View>

        <View style={styles.form}>
          <Input
            label="Password"
            value={password}
            onChangeText={setPassword}
            secureTextEntry
            placeholder="Confirm it is you"
          />

          <Pressable
            style={styles.confirmRow}
            onPress={() => setConfirmed((current) => !current)}
            accessibilityRole="checkbox"
            accessibilityState={{ checked: confirmed }}
            accessibilityLabel="I understand what happens to my account"
          >
            {confirmed ? (
              <CheckSquare color={colors.state.danger} size={22} weight="bold" />
            ) : (
              <Square color={colors.text.tertiary} size={22} weight="bold" />
            )}
            <Text style={[typeScale.bodyS, styles.confirmLabel]}>
              I understand what happens to my account.
            </Text>
          </Pressable>

          {error !== undefined ? <Text style={[typeScale.body, styles.error]}>{error}</Text> : null}
        </View>

        <View style={styles.actions}>
          <Button
            variant="danger"
            fullWidth
            label="Delete my account"
            loading={submitting}
            onPress={() => {
              void handleDelete();
            }}
          />

          <Button variant="ghost" fullWidth label="Keep my account" onPress={onBack} />
        </View>
      </ScrollView>
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    actions: {
      gap: spacing.sm,
      marginTop: spacing.xl,
    },
    confirmLabel: {
      color: colors.text.secondary,
      flex: 1,
    },
    confirmRow: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.sm,
    },
    consequence: {
      color: colors.text.secondary,
    },
    consequences: {
      gap: spacing.md,
      marginBottom: spacing.xl,
    },
    container: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    content: {
      paddingBottom: spacing.xxxl,
      paddingHorizontal: layout.screenPaddingReading,
      paddingTop: spacing.lg,
    },
    error: {
      color: colors.state.danger,
    },
    form: {
      gap: spacing.lg,
    },
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    intro: {
      gap: spacing.sm,
      marginBottom: spacing.lg,
    },
    subtitle: {
      color: colors.text.secondary,
    },
    title: {
      color: colors.text.primary,
    },
  });

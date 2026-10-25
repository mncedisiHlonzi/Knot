import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, FlatList, Pressable, StyleSheet, Text, View } from 'react-native';
import { ArrowLeft } from 'phosphor-react-native';

import { listSessions, revokeSession, type SessionSummary } from '../../api/auth';
import { describeError } from '../../api/client';
import Button from '../../components/Button';
import EmptyState from '../../components/EmptyState';
import Header from '../../components/Header';
import {
  layout,
  spacing,
  typeScale,
  useTheme,
  useThemedStyles,
  type ThemeColors,
} from '../../theme';
import { describeDevice, lastUsedLabel } from '../../utils/sessionFormat';

type SessionsScreenProps = {
  /** The signed-in person's access token. */
  readonly token: string;
  /** Called when the person returns to settings. */
  readonly onBack: () => void;
};

/**
 * The devices a person is signed in on.
 *
 * The list comes straight from `GET /auth/sessions`, which returns live sessions
 * newest first and marks the one the request was made from. That mark is what makes
 * the screen safe to act on: the session you are reading the list on has no Revoke
 * button, so a stray tap cannot sign you out of the app you are holding.
 *
 * Revoking is optimistic: the row is removed on success, and on failure the list is
 * left as it was and the reason is shown above it. Re-reading the whole list after
 * every revocation would be more truthful but would also throw away the scroll
 * position on a screen whose only content is that list.
 */
export default function SessionsScreen({ token, onBack }: SessionsScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [sessions, setSessions] = useState<readonly SessionSummary[] | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);
  const [revoking, setRevoking] = useState<string | undefined>(undefined);

  const load = useCallback(async (): Promise<void> => {
    try {
      const page = await listSessions(token);
      setSessions(page.sessions);
    } catch (caught) {
      setError(describeError(caught));
      setSessions([]);
    }
  }, [token]);

  useEffect(() => {
    void load();
  }, [load]);

  async function handleRevoke(session: SessionSummary): Promise<void> {
    setError(undefined);
    setRevoking(session.id);

    try {
      await revokeSession(session.id, token);
      setSessions((current) => (current ?? []).filter((entry) => entry.id !== session.id));
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setRevoking(undefined);
    }
  }

  return (
    <View style={styles.container}>
      <Header
        title="Sessions"
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

      {sessions === undefined ? (
        <View style={styles.loading}>
          <ActivityIndicator color={colors.text.tertiary} />
        </View>
      ) : (
        <FlatList
          data={sessions}
          keyExtractor={(session) => session.id}
          contentContainerStyle={styles.content}
          ListHeaderComponent={
            <>
              <Text style={[typeScale.bodyS, styles.intro]}>
                These are the devices signed in to your account. Revoke any you no longer use.
              </Text>
              {error === undefined ? null : (
                <Text style={[typeScale.bodyS, styles.error]}>{error}</Text>
              )}
            </>
          }
          ListEmptyComponent={
            <EmptyState
              headline="No signed-in devices"
              body="You are not signed in anywhere else."
            />
          }
          ListFooterComponent={
            sessions.length === 0 ? null : (
              <Text style={[typeScale.bodyS, styles.footer]}>
                Up to 5 devices. Signing in on a sixth signs the oldest one out.
              </Text>
            )
          }
          renderItem={({ item }) => (
            <View style={styles.row}>
              <View style={styles.rowText}>
                <Text style={[typeScale.body, styles.device]} numberOfLines={1}>
                  {describeDevice(item.device_info)}
                </Text>
                <Text style={[typeScale.bodyS, styles.meta]}>
                  {item.current ? 'This device' : lastUsedLabel(item.last_used_at)}
                </Text>
                {item.ip_address === '' ? null : (
                  <Text style={[typeScale.bodyS, styles.meta]}>{item.ip_address}</Text>
                )}
              </View>

              {item.current ? (
                <Text style={[typeScale.overline, styles.currentBadge]}>Current</Text>
              ) : (
                <Button
                  variant="ghost"
                  label="Revoke"
                  loading={revoking === item.id}
                  onPress={() => {
                    void handleRevoke(item);
                  }}
                />
              )}
            </View>
          )}
        />
      )}
    </View>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    container: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    content: {
      paddingBottom: spacing.xxxl,
      paddingHorizontal: layout.screenPaddingReading,
      paddingTop: spacing.lg,
    },
    currentBadge: {
      color: colors.text.tertiary,
    },
    device: {
      color: colors.text.primary,
    },
    error: {
      color: colors.state.danger,
      marginBottom: spacing.md,
    },
    footer: {
      color: colors.text.tertiary,
      marginTop: spacing.lg,
    },
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    intro: {
      color: colors.text.secondary,
      marginBottom: spacing.lg,
    },
    loading: {
      alignItems: 'center',
      flex: 1,
      justifyContent: 'center',
    },
    meta: {
      color: colors.text.tertiary,
    },
    row: {
      alignItems: 'center',
      borderBottomColor: colors.bg.separator,
      borderBottomWidth: 4,
      flexDirection: 'row',
      gap: spacing.md,
      paddingVertical: spacing.lg,
    },
    rowText: {
      flex: 1,
      gap: spacing.xs,
    },
  });

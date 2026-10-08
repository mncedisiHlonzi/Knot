import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { User, describeError } from '../../api/client';
import { RootedSignal, rootedApi } from '../../api/rooted';
import RootedBadge from '../../components/RootedBadge';
import { colors, fontSizes, fontWeights, radius, spacing } from '../../theme';

type ProfileScreenProps = {
  /** The id of the user whose profile to show. */
  readonly userId: string;
  /** The signed-in user's access token, needed to read the current user's signals. */
  readonly token: string;
  /** The signed-in user, so their own name and email can be shown without a lookup. */
  readonly currentUser: User;
  /** Called when the person wants to set or replace their Rooted signal. */
  readonly onSetRooted: () => void;
  /** Called when the person returns to the previous screen. */
  readonly onBack: () => void;
};

/**
 * A person's profile: who they are, and where they are Rooted.
 *
 * A user's own profile reads `GET /users/me/rooted`, so it shows private signals
 * too. Anyone else's profile reads `GET /users/{id}/rooted`, which returns only
 * public signals, and there is no endpoint for another user's name yet, so their
 * metadata is not shown.
 */
export default function ProfileScreen({
  userId,
  token,
  currentUser,
  onSetRooted,
  onBack,
}: ProfileScreenProps): React.ReactElement {
  const isOwn = currentUser.id === userId;

  const [signals, setSignals] = useState<readonly RootedSignal[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const result = isOwn
        ? await rootedApi.getMySignals(token)
        : await rootedApi.getUserSignals(userId);
      setSignals(result.signals);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [isOwn, token, userId]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <ScrollView contentContainerStyle={styles.content}>
      <Pressable style={styles.link} onPress={onBack}>
        <Text style={styles.linkText}>Back</Text>
      </Pressable>

      {isOwn ? (
        <>
          <Text style={styles.title}>{currentUser.display_name}</Text>
          <Text style={styles.meta}>{currentUser.email}</Text>
        </>
      ) : (
        <Text style={styles.title}>Profile</Text>
      )}

      <Text style={styles.sectionTitle}>Rooted</Text>

      {loading ? <ActivityIndicator style={styles.spinner} /> : null}
      {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

      {!loading && error === undefined && signals.length === 0 ? (
        <Text style={styles.hint}>
          {isOwn
            ? 'You have not said where you are Rooted yet.'
            : 'This person has not shared a public Rooted signal.'}
        </Text>
      ) : null}

      {signals.map((signal) => (
        <View key={signal.id} style={styles.signalRow}>
          <RootedBadge place={signal.place} durationBucket={signal.duration_bucket} />
          {isOwn && !signal.is_public ? <Text style={styles.privateTag}>private</Text> : null}
        </View>
      ))}

      {isOwn ? (
        <Pressable style={styles.primaryButton} onPress={onSetRooted}>
          <Text style={styles.primaryButtonText}>Set Rooted</Text>
        </Pressable>
      ) : null}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  content: {
    backgroundColor: colors.bg.primary,
    padding: spacing.xl,
    paddingBottom: spacing['3xl'],
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.base,
    marginTop: spacing.lg,
  },
  hint: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: spacing.sm,
  },
  link: {
    marginBottom: spacing.md,
  },
  linkText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
  },
  meta: {
    color: colors.text.secondary,
    fontSize: fontSizes.base,
    marginTop: spacing.xs,
  },
  primaryButton: {
    alignItems: 'center',
    backgroundColor: colors.brand.purple,
    borderRadius: radius.md,
    marginTop: spacing.xl,
    paddingVertical: spacing.md,
  },
  primaryButtonText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  privateTag: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginLeft: spacing.sm,
  },
  sectionTitle: {
    color: colors.text.primary,
    fontSize: fontSizes.lg,
    fontWeight: fontWeights.bold,
    marginTop: spacing.xl,
  },
  signalRow: {
    alignItems: 'center',
    flexDirection: 'row',
    marginTop: spacing.md,
  },
  spinner: {
    marginTop: spacing.xl,
  },
  title: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
  },
});

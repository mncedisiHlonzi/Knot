import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Image,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';

import { User, describeError } from '../../api/client';
import { RootedSignal, rootedApi } from '../../api/rooted';
import { uploadAvatar } from '../../api/users';
import MediaPickerSheet, { PickedMedia } from '../../components/MediaPickerSheet';
import RootedBadge from '../../components/RootedBadge';
import { API_BASE_URL } from '../../config/api';
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
  /** Called with the updated user after a successful avatar upload. */
  readonly onUserUpdated: (user: User) => void;
  /** Called when the person returns to the previous screen. */
  readonly onBack: () => void;
};

/**
 * The initials to show when a user has no avatar: the first letter of their first
 * two words, upper-cased. Falls back to the email's first letter, then "?".
 */
function initialsFor(user: User): string {
  const words = user.display_name
    .trim()
    .split(/\s+/)
    .filter((word) => word !== '');
  if (words.length >= 2) {
    return (words[0][0] + words[1][0]).toUpperCase();
  }
  if (words.length === 1) {
    return words[0].slice(0, 2).toUpperCase();
  }
  return (user.email[0] ?? '?').toUpperCase();
}

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
  onUserUpdated,
  onBack,
}: ProfileScreenProps): React.ReactElement {
  const isOwn = currentUser.id === userId;

  const [signals, setSignals] = useState<readonly RootedSignal[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);
  const [pickerVisible, setPickerVisible] = useState(false);
  const [uploadingAvatar, setUploadingAvatar] = useState(false);
  const [avatarError, setAvatarError] = useState<string | undefined>(undefined);

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

  /**
   * Uploads the chosen image as the avatar. The response is the updated user, so
   * the caller can re-render with the new URL without a second request.
   */
  const handleAvatarPicked = useCallback(
    async (media: PickedMedia): Promise<void> => {
      setAvatarError(undefined);
      setUploadingAvatar(true);
      try {
        const updated = await uploadAvatar(media.uri, media.mimeType, token);
        onUserUpdated(updated);
      } catch (caught) {
        setAvatarError(describeError(caught));
      } finally {
        setUploadingAvatar(false);
      }
    },
    [onUserUpdated, token],
  );

  const avatarUri =
    currentUser.avatar_url === '' ? undefined : `${API_BASE_URL}${currentUser.avatar_url}`;

  return (
    <ScrollView contentContainerStyle={styles.content}>
      <Pressable style={styles.link} onPress={onBack}>
        <Text style={styles.linkText}>Back</Text>
      </Pressable>

      {isOwn ? (
        <>
          <View style={styles.avatarRow}>
            {avatarUri === undefined ? (
              <View style={styles.avatarFallback}>
                <Text style={styles.avatarInitials}>{initialsFor(currentUser)}</Text>
              </View>
            ) : (
              <Image
                style={styles.avatar}
                source={{ uri: avatarUri }}
                accessibilityLabel="your profile picture"
              />
            )}
          </View>
          <Text style={styles.title}>{currentUser.display_name}</Text>
          <Text style={styles.meta}>{currentUser.email}</Text>

          <Pressable
            style={[styles.secondaryButton, uploadingAvatar ? styles.buttonDisabled : null]}
            onPress={() => {
              setAvatarError(undefined);
              setPickerVisible(true);
            }}
            disabled={uploadingAvatar}
          >
            <Text style={styles.secondaryButtonText}>
              {uploadingAvatar
                ? 'Uploading…'
                : currentUser.avatar_url === ''
                  ? 'Add avatar'
                  : 'Edit avatar'}
            </Text>
          </Pressable>
          {avatarError !== undefined ? <Text style={styles.error}>{avatarError}</Text> : null}

          <MediaPickerSheet
            visible={pickerVisible}
            allowVideo={false}
            onClose={() => setPickerVisible(false)}
            onPicked={(media) => {
              void handleAvatarPicked(media);
            }}
            onError={setAvatarError}
          />
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
  avatar: {
    borderColor: colors.border.default,
    borderRadius: radius.pill,
    borderWidth: 1,
    height: 96,
    width: 96,
  },
  avatarFallback: {
    alignItems: 'center',
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.default,
    borderRadius: radius.pill,
    borderWidth: 1,
    height: 96,
    justifyContent: 'center',
    width: 96,
  },
  avatarInitials: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
  },
  avatarRow: {
    alignItems: 'center',
    marginBottom: spacing.md,
  },
  buttonDisabled: {
    opacity: 0.5,
  },
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
  secondaryButton: {
    alignItems: 'center',
    borderColor: colors.border.default,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.md,
    paddingVertical: spacing.md,
  },
  secondaryButtonText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
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

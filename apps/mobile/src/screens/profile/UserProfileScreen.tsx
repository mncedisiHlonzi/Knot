import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Image,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  View,
} from 'react-native';

import { User, describeError } from '../../api/client';
import { conversationsApi } from '../../api/conversations';
import { Activity, PROFILE_PAGE_SIZE, ProfileUser, profileApi } from '../../api/profile';
import { uploadAvatar } from '../../api/users';
import MediaPickerSheet, { PickedMedia } from '../../components/MediaPickerSheet';
import RootedBadge from '../../components/RootedBadge';
import { API_BASE_URL } from '../../config/api';
import { languageName } from '../../data/languages';
import { colors, fontSizes, fontWeights, lineHeights, radius, spacing } from '../../theme';
import { getInitials } from '../../utils/initials';
import { formatRelativeTime } from '../../utils/time';

type UserProfileScreenProps = {
  /** The user whose wall to show. */
  readonly userId: string;
  /** The signed-in user's access token, needed only for the owner's avatar upload. */
  readonly token: string;
  /** True when the wall belongs to the signed-in user, which shows owner controls. */
  readonly isOwnProfile: boolean;
  /** Called to open a story (a story activity, or the story an adaptation belongs to). */
  readonly onOpenStory: (storyId: string) => void;
  /** Called to open a comment thread. */
  readonly onOpenComment: (storyId: string, versionId: string) => void;
  /** Called with the updated user after a successful avatar upload. */
  readonly onUserUpdated: (user: User) => void;
  /** Called when the owner wants to set or replace their Rooted signal. */
  readonly onSetRooted: () => void;
  /** Called when the owner chooses to sign out. */
  readonly onSignOut: () => void;
  /**
   * Called when the person returns to the previous screen.
   *
   * Omit it, along with `showBackButton`, when this screen is a tab root: there is
   * nothing above the tab bar to go back to.
   */
  readonly onBack?: () => void;
  /**
   * Whether to show the "← Back" link. Defaults to true.
   *
   * The link is only rendered when `onBack` is given, so the Profile tab, which
   * is a destination rather than a pushed screen, shows no way back.
   */
  readonly showBackButton?: boolean;
};

/** The label a wall card shows for each activity kind. */
const KIND_LABELS: Readonly<Record<Activity['kind'], string>> = {
  story: 'Story',
  version: 'Adaptation',
  comment: 'Comment',
  bridge: 'Bridge',
};

/** The headline for one activity: the most useful single line of its payload. */
function activityHeadline(activity: Activity): string {
  switch (activity.kind) {
    case 'story':
      return activity.payload.title;
    case 'version':
      return activity.payload.story_title;
    case 'comment':
      return activity.payload.body_preview;
    case 'bridge':
      return `Bridged into ${languageName(activity.payload.target_language)}`;
    default:
      return '';
  }
}

/** The secondary line for one activity, or "" when the payload names nothing more. */
function activityMeta(activity: Activity): string {
  switch (activity.kind) {
    case 'story':
      return `${activity.payload.pillar} · ${languageName(activity.payload.language)}`;
    case 'version':
      return `Adapted into ${languageName(activity.payload.language)}`;
    case 'comment':
      return 'On a version';
    case 'bridge':
      return 'From a comment';
    default:
      return '';
  }
}

/**
 * One user's public wall: who they are, and everything they have authored —
 * stories, adaptations, comments, and bridges — newest first.
 *
 * It is the public variant of the profile: it reads `GET /users/{id}/profile`,
 * which needs no token, and the owner's variant adds only the avatar upload,
 * Set Rooted, and sign-out controls. A non-owner sees exactly the same wall
 * without them.
 *
 * The same screen is both the Profile tab (KNOT-ADR-044) and a pushed overlay:
 * the Profile tab renders it without a back link, because a tab is a destination
 * rather than something to return from. One screen means the owner's own wall and
 * the wall everyone else sees can never drift apart.
 */
export default function UserProfileScreen({
  userId,
  token,
  isOwnProfile,
  onOpenStory,
  onOpenComment,
  onUserUpdated,
  onSetRooted,
  onSignOut,
  onBack,
  showBackButton = true,
}: UserProfileScreenProps): React.ReactElement {
  const [profile, setProfile] = useState<ProfileUser | undefined>(undefined);
  const [activities, setActivities] = useState<readonly Activity[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const [pickerVisible, setPickerVisible] = useState(false);
  const [uploadingAvatar, setUploadingAvatar] = useState(false);
  const [avatarError, setAvatarError] = useState<string | undefined>(undefined);

  const loadFirstPage = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const page = await profileApi.getUserProfile(userId, undefined, PROFILE_PAGE_SIZE);
      setProfile(page.user);
      setActivities(page.activities);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [userId]);

  useEffect(() => {
    void loadFirstPage();
  }, [loadFirstPage]);

  const refresh = useCallback(async (): Promise<void> => {
    setRefreshing(true);
    setError(undefined);

    try {
      const page = await profileApi.getUserProfile(userId, undefined, PROFILE_PAGE_SIZE);
      setProfile(page.user);
      setActivities(page.activities);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setRefreshing(false);
    }
  }, [userId]);

  const loadMore = useCallback(async (): Promise<void> => {
    if (nextCursor === '' || loadingMore) {
      return;
    }

    setLoadingMore(true);
    setError(undefined);

    try {
      const page = await profileApi.getUserProfile(userId, nextCursor, PROFILE_PAGE_SIZE);
      // Append rather than replace: the cursor is exclusive, so a page never
      // repeats an activity already on screen.
      setActivities((existing) => [...existing, ...page.activities]);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoadingMore(false);
    }
  }, [loadingMore, nextCursor, userId]);

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

  /**
   * Opens the entity an activity points at.
   *
   * A story opens the story. An adaptation opens the story it belongs to. A
   * comment opens its version's thread (the payload already names both ids). A
   * bridge names only its source comment and version, so the story is resolved
   * through `GET /comments/{id}` before the thread opens.
   */
  const handleOpenActivity = useCallback(
    (activity: Activity): void => {
      switch (activity.kind) {
        case 'story':
          onOpenStory(activity.id);
          return;
        case 'version':
          onOpenStory(activity.payload.story_id);
          return;
        case 'comment':
          onOpenComment(activity.payload.story_id, activity.payload.version_id);
          return;
        case 'bridge':
          void (async () => {
            try {
              const { comment } = await conversationsApi.getComment(
                activity.payload.source_comment_id,
              );
              onOpenComment(comment.story_id, comment.version_id);
            } catch {
              // A comment that can no longer be read simply does not open.
            }
          })();
          return;
        default:
          return;
      }
    },
    [onOpenComment, onOpenStory],
  );

  const avatarUri =
    profile?.avatar_url === undefined || profile?.avatar_url === null || profile.avatar_url === ''
      ? undefined
      : `${API_BASE_URL}${profile.avatar_url}`;

  return (
    <FlatList
      data={activities}
      keyExtractor={(activity) => activity.id}
      contentContainerStyle={styles.content}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={refresh}
          tintColor={colors.text.secondary}
        />
      }
      ListHeaderComponent={
        <View>
          {showBackButton && onBack !== undefined ? (
            <Pressable style={styles.link} onPress={onBack}>
              <Text style={styles.linkText}>← Back</Text>
            </Pressable>
          ) : null}

          {loading ? <ActivityIndicator style={styles.spinner} /> : null}
          {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

          {profile !== undefined ? (
            <View style={styles.header}>
              {avatarUri === undefined ? (
                <View style={styles.avatarFallback}>
                  <Text style={styles.avatarInitials}>{getInitials(profile.display_name)}</Text>
                </View>
              ) : (
                <Image
                  style={styles.avatar}
                  source={{ uri: avatarUri }}
                  accessibilityLabel={`${profile.display_name}'s profile picture`}
                />
              )}

              <Text style={styles.name}>{profile.display_name}</Text>

              {profile.rooted !== null && profile.rooted !== undefined ? (
                <View style={styles.rootedRow}>
                  <RootedBadge
                    place={profile.rooted.place}
                    durationBucket={profile.rooted.duration_bucket}
                  />
                </View>
              ) : null}

              <Text style={styles.joined}>Joined {formatRelativeTime(profile.joined_at)}</Text>

              {isOwnProfile ? (
                <View style={styles.ownerButtons}>
                  <Pressable
                    style={[styles.primaryButton, uploadingAvatar ? styles.disabled : null]}
                    onPress={() => {
                      setAvatarError(undefined);
                      setPickerVisible(true);
                    }}
                    disabled={uploadingAvatar}
                  >
                    <Text style={styles.primaryButtonText}>
                      {uploadingAvatar ? 'Uploading…' : 'Edit avatar'}
                    </Text>
                  </Pressable>
                  <Pressable style={styles.secondaryButton} onPress={onSetRooted}>
                    <Text style={styles.secondaryButtonText}>Set Rooted</Text>
                  </Pressable>
                  <Pressable style={styles.secondaryButton} onPress={onSignOut}>
                    <Text style={styles.secondaryButtonText}>Sign out</Text>
                  </Pressable>
                </View>
              ) : null}

              {avatarError !== undefined ? <Text style={styles.error}>{avatarError}</Text> : null}

              <MediaPickerSheet
                visible={pickerVisible}
                allowVideo={false}
                onClose={() => setPickerVisible(false)}
                onPicked={(picked) => {
                  // An avatar is a single image, so only the first is used.
                  const first = picked[0];
                  if (first !== undefined) {
                    void handleAvatarPicked(first);
                  }
                }}
                onError={setAvatarError}
              />

              <Text style={styles.sectionTitle}>Activity</Text>
            </View>
          ) : null}
        </View>
      }
      renderItem={({ item }) => (
        <Pressable style={styles.card} onPress={() => handleOpenActivity(item)}>
          <Text style={styles.cardKind}>{KIND_LABELS[item.kind]}</Text>
          <Text style={styles.cardHeadline}>{activityHeadline(item)}</Text>
          <Text style={styles.cardMeta}>
            {activityMeta(item)} · {formatRelativeTime(item.created_at)}
          </Text>
        </Pressable>
      )}
      ListEmptyComponent={
        !loading && error === undefined ? <Text style={styles.hint}>Nothing here yet.</Text> : null
      }
      ListFooterComponent={
        nextCursor !== '' ? (
          <Pressable
            style={[styles.secondaryButton, loadingMore ? styles.disabled : null]}
            onPress={loadMore}
            disabled={loadingMore}
          >
            <Text style={styles.secondaryButtonText}>{loadingMore ? 'Loading…' : 'Load more'}</Text>
          </Pressable>
        ) : null
      }
    />
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
  card: {
    backgroundColor: colors.bg.surface,
    borderColor: colors.border.subtle,
    borderRadius: radius.md,
    borderWidth: 1,
    marginTop: spacing.md,
    padding: spacing.lg,
  },
  cardHeadline: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    lineHeight: lineHeights.base,
    marginTop: spacing.xs,
  },
  cardKind: {
    color: colors.text.brand,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.semiBold,
  },
  cardMeta: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: spacing.xs,
  },
  content: {
    backgroundColor: colors.bg.primary,
    padding: spacing.xl,
    paddingBottom: spacing['3xl'],
  },
  disabled: {
    opacity: 0.5,
  },
  error: {
    color: colors.state.error,
    fontSize: fontSizes.base,
    marginTop: spacing.lg,
  },
  header: {
    alignItems: 'center',
  },
  hint: {
    color: colors.text.secondary,
    fontSize: fontSizes.base,
    marginTop: spacing.lg,
  },
  joined: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginTop: spacing.sm,
  },
  link: {
    alignSelf: 'flex-start',
    marginBottom: spacing.md,
  },
  linkText: {
    color: colors.text.brand,
    fontSize: fontSizes.base,
  },
  name: {
    color: colors.text.primary,
    fontSize: fontSizes.xl,
    fontWeight: fontWeights.bold,
    marginTop: spacing.md,
  },
  ownerButtons: {
    marginTop: spacing.lg,
    width: '100%',
  },
  primaryButton: {
    alignItems: 'center',
    backgroundColor: colors.brand.purple,
    borderRadius: radius.md,
    paddingVertical: spacing.md,
  },
  primaryButtonText: {
    color: colors.text.primary,
    fontSize: fontSizes.base,
    fontWeight: fontWeights.semiBold,
  },
  rootedRow: {
    marginTop: spacing.sm,
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
    alignSelf: 'flex-start',
    color: colors.text.primary,
    fontSize: fontSizes.lg,
    fontWeight: fontWeights.bold,
    marginTop: spacing.xl,
  },
  spinner: {
    marginTop: spacing.xl,
  },
});

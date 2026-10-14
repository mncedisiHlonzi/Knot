import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Animated,
  FlatList,
  Modal,
  PanResponder,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import {
  ArrowLeft,
  Camera,
  CaretRight,
  DotsThree,
  Plant,
  Prohibit,
  SignOut,
  type Icon,
} from 'phosphor-react-native';

import { User, describeError } from '../../api/client';
import { conversationsApi } from '../../api/conversations';
import { BlockedUser, blocksApi, describeModerationError } from '../../api/moderation';
import {
  Activity,
  ActivityKind,
  PROFILE_PAGE_SIZE,
  ProfileUser,
  profileApi,
} from '../../api/profile';
import { uploadAvatar } from '../../api/users';
import Avatar from '../../components/Avatar';
import Button from '../../components/Button';
import EmptyState from '../../components/EmptyState';
import Header from '../../components/Header';
import MediaPickerSheet, { PickedMedia } from '../../components/MediaPickerSheet';
import PillarTag from '../../components/PillarTag';
import RootedBadge from '../../components/RootedBadge';
import { API_BASE_URL } from '../../config/api';
import { colors, elevation, layout, radius, spacing, tabularNums, typeScale } from '../../theme';
import { formatRelativeTime } from '../../utils/time';
import {
  DEFAULT_PROFILE_TAB,
  PROFILE_TABS,
  activityCard,
  filterActivities,
  profileStats,
  profileTabEmptyHeadline,
  statsLine,
} from './profileActivity';

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
  /**
   * Called to open a question. An inquiry activity opens its own question; an
   * inquiry_answer activity opens the question it answered.
   */
  readonly onOpenInquiry: (inquiryId: string) => void;
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
   * Whether to show the back control. Defaults to true.
   *
   * The control is only rendered when `onBack` is given, so the Profile tab, which
   * is a destination rather than a pushed screen, shows no way back.
   */
  readonly showBackButton?: boolean;
};

/** How far the options sheet must be dragged down before it dismisses. */
const SHEET_CLOSE_DISTANCE = 96;

/** The empty-state illustration for one tab. Versions reuse the stories artwork. */
function emptyIllustration(kind: ActivityKind): 'no-stories' | 'no-inquiries' {
  return kind === 'inquiry' ? 'no-inquiries' : 'no-stories';
}

/**
 * One row in the profile options sheet: a leading icon, a label, and a trailing
 * chevron — swapped for a spinner while the row's action is in flight.
 *
 * It is local to this file rather than a shared component, because the options
 * sheet is the only menu the profile draws (KNOT-020g-fix).
 */
function OptionRow({
  icon,
  label,
  danger = false,
  busy = false,
  onPress,
}: {
  readonly icon: Icon;
  readonly label: string;
  readonly danger?: boolean;
  readonly busy?: boolean;
  readonly onPress: () => void;
}): React.ReactElement {
  const IconComponent = icon;
  return (
    <Pressable
      style={styles.option}
      onPress={onPress}
      disabled={busy}
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ busy, disabled: busy }}
    >
      <IconComponent color={danger ? colors.state.danger : colors.text.tertiary} size={20} />
      <Text
        style={[typeScale.body, styles.optionLabel, danger ? styles.optionLabelDanger : null]}
        numberOfLines={1}
      >
        {label}
      </Text>
      {busy ? (
        <ActivityIndicator size="small" color={colors.text.tertiary} />
      ) : (
        <CaretRight color={colors.text.tertiary} size={20} />
      )}
    </Pressable>
  );
}

/**
 * One person's wall: who they are, and what they authored — the stories they
 * told, the adaptations they wrote, and the questions they asked — newest first.
 *
 * The header block is centred: avatar, name, the Rooted badge when there is one,
 * when they joined, and a stats line. The owner sees "Edit profile" and an
 * overflow control that opens a bottom-sheet menu — Edit avatar, Set Rooted,
 * Blocked users, and Sign out — while a visitor's opens Block user. Below the
 * block, three tabs (Stories, Versions, Inquiries) filter the wall, and each
 * entry is a card: an overline (or the story's pillar tag), the title, and a meta
 * line, closed by a full-bleed hairline — the design reference's tab content.
 *
 * The wall shows only what the person authored themselves. The other activity kinds
 * — comments, bridges, and answers — still arrive on the stream (the API is
 * unchanged) but are filtered out of the display and given no tab (KNOT-020g-fix).
 *
 * The stats and the per-tab lists are computed over the activities loaded so far
 * — the profile API returns no totals and no per-kind filter, so both grow as the
 * wall is paged (KNOT-020c disclosure).
 *
 * It is the public variant of the profile: it reads `GET /users/{id}/profile`,
 * which needs no token, and the owner's variant adds only the avatar upload,
 * Set Rooted, and sign-out controls.
 *
 * The same screen is both the Profile tab (KNOT-ADR-044) and a pushed overlay:
 * the tab renders it without a back control, because a tab is a destination
 * rather than something to return from.
 */
export default function UserProfileScreen({
  userId,
  token,
  isOwnProfile,
  onOpenStory,
  onOpenComment,
  onOpenInquiry,
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
  const [activeTab, setActiveTab] = useState<ActivityKind>(DEFAULT_PROFILE_TAB);
  const [menuOpen, setMenuOpen] = useState(false);

  const [pickerVisible, setPickerVisible] = useState(false);
  const [uploadingAvatar, setUploadingAvatar] = useState(false);
  const [avatarError, setAvatarError] = useState<string | undefined>(undefined);

  const [blocking, setBlocking] = useState(false);
  const [blockError, setBlockError] = useState<string | undefined>(undefined);
  const [showBlocked, setShowBlocked] = useState(false);
  const [blockedUsers, setBlockedUsers] = useState<readonly BlockedUser[]>([]);
  const [blockedLoading, setBlockedLoading] = useState(false);

  // The options sheet. It dismisses on a downward drag of the handle, in addition
  // to the backdrop tap and the Android hardware back button.
  const dragY = useRef(new Animated.Value(0)).current;
  const closeMenu = useCallback((): void => {
    dragY.setValue(0);
    setMenuOpen(false);
  }, [dragY]);
  const sheetPan = useMemo(
    () =>
      PanResponder.create({
        onStartShouldSetPanResponder: () => true,
        onMoveShouldSetPanResponder: (_event, gesture) => gesture.dy > 2,
        onPanResponderMove: (_event, gesture) => {
          if (gesture.dy > 0) {
            dragY.setValue(gesture.dy);
          }
        },
        onPanResponderRelease: (_event, gesture) => {
          if (gesture.dy > SHEET_CLOSE_DISTANCE || gesture.vy > 0.5) {
            closeMenu();
          } else {
            Animated.spring(dragY, { toValue: 0, useNativeDriver: true }).start();
          }
        },
      }),
    [closeMenu, dragY],
  );

  const loadFirstPage = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(undefined);

    try {
      const page = await profileApi.getUserProfile(userId, undefined, PROFILE_PAGE_SIZE, token);
      setProfile(page.user);
      setActivities(page.activities);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoading(false);
    }
  }, [token, userId]);

  useEffect(() => {
    void loadFirstPage();
  }, [loadFirstPage]);

  const refresh = useCallback(async (): Promise<void> => {
    setRefreshing(true);
    setError(undefined);

    try {
      const page = await profileApi.getUserProfile(userId, undefined, PROFILE_PAGE_SIZE, token);
      setProfile(page.user);
      setActivities(page.activities);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setRefreshing(false);
    }
  }, [token, userId]);

  const loadMore = useCallback(async (): Promise<void> => {
    if (nextCursor === '' || loadingMore) {
      return;
    }

    setLoadingMore(true);
    setError(undefined);

    try {
      const page = await profileApi.getUserProfile(userId, nextCursor, PROFILE_PAGE_SIZE, token);
      // Append rather than replace: the cursor is exclusive, so a page never
      // repeats an activity already on screen.
      setActivities((existing) => [...existing, ...page.activities]);
      setNextCursor(page.next_cursor);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setLoadingMore(false);
    }
  }, [loadingMore, nextCursor, token, userId]);

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
   * Blocks the profile's owner after a confirmation, then returns to the previous
   * screen: the wall is now hidden, so leaving it is the honest response.
   */
  const handleBlock = useCallback((): void => {
    const name = profile?.display_name ?? 'this person';
    Alert.alert(
      `Block ${name}?`,
      'Their content will be hidden from you and they will not be able to interact with your content.',
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Block',
          style: 'destructive',
          onPress: () => {
            void (async () => {
              setBlocking(true);
              setBlockError(undefined);
              try {
                await blocksApi.block(userId, token);
                onBack?.();
              } catch (caught) {
                setBlockError(describeModerationError(caught));
              } finally {
                setBlocking(false);
              }
            })();
          },
        },
      ],
    );
  }, [onBack, profile, token, userId]);

  /** Loads the owner's block list, used by the "Blocked users" section. */
  const loadBlockedUsers = useCallback(async (): Promise<void> => {
    setBlockedLoading(true);
    setBlockError(undefined);
    try {
      const page = await blocksApi.listBlocks(token, undefined, 50);
      setBlockedUsers(page.blocks.map((entry) => entry.user));
    } catch (caught) {
      setBlockError(describeModerationError(caught));
    } finally {
      setBlockedLoading(false);
    }
  }, [token]);

  const toggleBlockedUsers = useCallback((): void => {
    setShowBlocked((current) => {
      const next = !current;
      if (next) {
        void loadBlockedUsers();
      }
      return next;
    });
  }, [loadBlockedUsers]);

  const handleUnblock = useCallback(
    async (blockedID: string): Promise<void> => {
      setBlockError(undefined);
      try {
        await blocksApi.unblock(blockedID, token);
        setBlockedUsers((current) => current.filter((user) => user.id !== blockedID));
      } catch (caught) {
        setBlockError(describeModerationError(caught));
      }
    },
    [token],
  );

  /**
   * Opens the entity an activity points at.
   *
   * A story opens the story. An adaptation opens the story it belongs to. A
   * comment opens its version's thread (the payload already names both ids). A
   * bridge names only its source comment and version, so the story is resolved
   * through `GET /comments/{id}` before the thread opens. A question and an answer
   * both open the question: the payload names the inquiry in each case, so no
   * lookup is needed (KNOT-ADR-057).
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
                token,
              );
              onOpenComment(comment.story_id, comment.version_id);
            } catch {
              // A comment that can no longer be read simply does not open.
            }
          })();
          return;
        case 'inquiry':
          onOpenInquiry(activity.id);
          return;
        case 'inquiry_answer':
          // An answer has no screen of its own: it belongs to the question it
          // answers, which is where the reader wants to land.
          onOpenInquiry(activity.payload.inquiry_id);
          return;
        default:
          return;
      }
    },
    [onOpenComment, onOpenInquiry, onOpenStory, token],
  );

  const avatarUri =
    profile?.avatar_url === undefined || profile?.avatar_url === null || profile.avatar_url === ''
      ? undefined
      : `${API_BASE_URL}${profile.avatar_url}`;

  const visible = filterActivities(activities, activeTab);

  return (
    <View style={styles.container}>
      <Header
        title="Profile"
        left={
          showBackButton && onBack !== undefined ? (
            <Pressable
              style={styles.iconButton}
              onPress={onBack}
              accessibilityRole="button"
              accessibilityLabel="Back"
            >
              <ArrowLeft color={colors.text.primary} size={24} />
            </Pressable>
          ) : undefined
        }
        right={
          <Pressable
            style={styles.iconButton}
            onPress={() => setMenuOpen(true)}
            accessibilityRole="button"
            accessibilityLabel="Profile options"
          >
            <DotsThree color={colors.text.primary} size={24} weight="bold" />
          </Pressable>
        }
      />

      <FlatList
        data={visible}
        keyExtractor={(activity) => activity.id}
        contentContainerStyle={styles.content}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={refresh}
            tintColor={colors.text.secondary}
          />
        }
        onEndReached={() => {
          void loadMore();
        }}
        onEndReachedThreshold={0.5}
        ListHeaderComponent={
          <View>
            {loading ? <ActivityIndicator style={styles.spinner} /> : null}
            {error !== undefined ? <Text style={styles.error}>{error}</Text> : null}

            {profile !== undefined ? (
              <View style={styles.profileBlock}>
                <View style={styles.avatarWrap}>
                  <Avatar uri={avatarUri} displayName={profile.display_name} size="xl" />
                </View>

                <Text style={[typeScale.h1, styles.name]}>{profile.display_name}</Text>

                {profile.rooted !== null && profile.rooted !== undefined ? (
                  <View style={styles.rootedRow}>
                    <RootedBadge
                      place={profile.rooted.place}
                      durationBucket={profile.rooted.duration_bucket}
                    />
                  </View>
                ) : null}

                <Text style={[typeScale.caption, styles.joined]}>
                  {`Joined ${formatRelativeTime(profile.joined_at)}`}
                </Text>

                <Text style={[typeScale.caption, tabularNums, styles.stats]}>
                  {statsLine(profileStats(activities))}
                </Text>

                {isOwnProfile ? (
                  <View style={styles.editProfile}>
                    <Button
                      variant="secondary"
                      fullWidth
                      label="Edit profile"
                      loading={uploadingAvatar}
                      onPress={() => {
                        setAvatarError(undefined);
                        setPickerVisible(true);
                      }}
                    />
                  </View>
                ) : null}

                {blockError !== undefined ? <Text style={styles.error}>{blockError}</Text> : null}
                {avatarError !== undefined ? <Text style={styles.error}>{avatarError}</Text> : null}

                {isOwnProfile && showBlocked ? (
                  <View style={styles.blockedList}>
                    {blockedLoading ? <ActivityIndicator /> : null}
                    {blockedUsers.map((user) => (
                      <View key={user.id} style={styles.blockedRow}>
                        <Text style={styles.blockedName} numberOfLines={1}>
                          {user.display_name}
                        </Text>
                        <Button
                          variant="secondary"
                          label="Unblock"
                          onPress={() => {
                            void handleUnblock(user.id);
                          }}
                        />
                      </View>
                    ))}
                    {!blockedLoading && blockedUsers.length === 0 ? (
                      <Text style={styles.hint}>You have not blocked anyone.</Text>
                    ) : null}
                  </View>
                ) : null}

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
              </View>
            ) : null}

            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              style={styles.tabs}
              contentContainerStyle={styles.tabsContent}
            >
              {PROFILE_TABS.map((tab) => {
                const active = tab.kind === activeTab;
                return (
                  <Pressable
                    key={tab.kind}
                    style={[styles.tab, active ? styles.tabActive : null]}
                    onPress={() => setActiveTab(tab.kind)}
                    accessibilityRole="button"
                    accessibilityState={{ selected: active }}
                    accessibilityLabel={tab.label}
                  >
                    <Text
                      style={[
                        typeScale.button,
                        styles.tabLabel,
                        active ? styles.tabLabelActive : null,
                      ]}
                    >
                      {tab.label}
                    </Text>
                  </Pressable>
                );
              })}
            </ScrollView>
          </View>
        }
        renderItem={({ item }) => {
          const card = activityCard(item, formatRelativeTime(item.created_at));
          if (card === undefined) {
            return null;
          }
          return (
            <Pressable
              style={styles.card}
              onPress={() => handleOpenActivity(item)}
              accessibilityRole="button"
              accessibilityLabel={card.title}
            >
              {card.pillar === undefined ? (
                card.overline === '' ? null : (
                  <Text style={[typeScale.overline, styles.cardOverline]} numberOfLines={1}>
                    {card.overline}
                  </Text>
                )
              ) : (
                <View style={styles.cardPillar}>
                  <PillarTag pillar={card.pillar} variant="plain" />
                </View>
              )}

              <Text style={[typeScale.h3, styles.cardTitle]} numberOfLines={2}>
                {card.title}
              </Text>

              {card.meta === '' ? null : (
                <Text style={[typeScale.caption, styles.cardMeta]} numberOfLines={1}>
                  {card.meta}
                </Text>
              )}
            </Pressable>
          );
        }}
        ListEmptyComponent={
          !loading && error === undefined ? (
            <EmptyState
              illustrationName={emptyIllustration(activeTab)}
              headline={profileTabEmptyHeadline(activeTab)}
            />
          ) : null
        }
        ListFooterComponent={loadingMore ? <ActivityIndicator style={styles.spinner} /> : null}
      />

      <Modal visible={menuOpen} transparent animationType="slide" onRequestClose={closeMenu}>
        <Pressable
          style={styles.sheetBackdrop}
          onPress={closeMenu}
          accessibilityRole="button"
          accessibilityLabel="Close the options"
        >
          <Animated.View style={[styles.sheet, { transform: [{ translateY: dragY }] }]}>
            {/* The handle is the drag target: a downward fling dismisses the
                sheet, anything else springs it back. */}
            <View style={styles.sheetHandleArea} {...sheetPan.panHandlers}>
              <View style={styles.sheetHandle} />
            </View>

            <Pressable style={styles.sheetBody} onPress={() => undefined}>
              {isOwnProfile ? (
                <>
                  <OptionRow
                    icon={Camera}
                    label="Edit avatar"
                    onPress={() => {
                      setMenuOpen(false);
                      setAvatarError(undefined);
                      setPickerVisible(true);
                    }}
                  />
                  <OptionRow
                    icon={Plant}
                    label="Set Rooted"
                    onPress={() => {
                      setMenuOpen(false);
                      onSetRooted();
                    }}
                  />
                  <OptionRow
                    icon={Prohibit}
                    label="Blocked users"
                    onPress={() => {
                      setMenuOpen(false);
                      toggleBlockedUsers();
                    }}
                  />
                  <OptionRow
                    icon={SignOut}
                    label="Sign out"
                    danger
                    onPress={() => {
                      setMenuOpen(false);
                      onSignOut();
                    }}
                  />
                </>
              ) : (
                <OptionRow
                  icon={Prohibit}
                  label="Block user"
                  busy={blocking}
                  onPress={handleBlock}
                />
              )}
            </Pressable>
          </Animated.View>
        </Pressable>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  avatarWrap: {
    alignItems: 'center',
    justifyContent: 'center',
  },
  blockedList: {
    marginTop: spacing.md,
    width: '100%',
  },
  blockedName: {
    color: colors.text.primary,
    flex: 1,
    marginRight: spacing.md,
  },
  blockedRow: {
    alignItems: 'center',
    borderTopColor: colors.border.hairline,
    borderTopWidth: 1,
    flexDirection: 'row',
    justifyContent: 'space-between',
    paddingVertical: spacing.sm,
  },
  // One activity card: the pillar tag or overline, the title, and the meta line,
  // closed by a full-bleed hairline (the design reference's tab content).
  card: {
    borderBottomColor: colors.border.hairline,
    borderBottomWidth: 1,
    marginHorizontal: -layout.screenPaddingFeed,
    paddingHorizontal: layout.screenPaddingFeed,
    paddingVertical: spacing.lg,
  },
  cardMeta: {
    color: colors.text.tertiary,
    marginTop: spacing.xs,
  },
  cardOverline: {
    color: colors.brand.primary,
  },
  cardPillar: {
    alignSelf: 'flex-start',
  },
  cardTitle: {
    color: colors.text.primary,
    marginTop: spacing.xs,
  },
  container: {
    backgroundColor: colors.bg.canvas,
    flex: 1,
  },
  content: {
    paddingBottom: spacing.xxxl,
    paddingHorizontal: layout.screenPaddingFeed,
  },
  editProfile: {
    alignSelf: 'stretch',
    marginTop: spacing.xl,
  },
  error: {
    color: colors.state.danger,
    marginTop: spacing.lg,
    textAlign: 'center',
  },
  hint: {
    color: colors.text.secondary,
    marginTop: spacing.md,
    textAlign: 'center',
  },
  iconButton: {
    alignItems: 'center',
    height: 44,
    justifyContent: 'center',
    width: 44,
  },
  joined: {
    color: colors.text.tertiary,
    marginTop: spacing.md,
  },
  name: {
    color: colors.text.primary,
    marginTop: spacing.lg,
    textAlign: 'center',
  },
  // One row of the options sheet: a leading icon, the label, and a trailing
  // chevron, separated from its neighbour by a hairline.
  option: {
    alignItems: 'center',
    borderTopColor: colors.border.hairline,
    borderTopWidth: 1,
    flexDirection: 'row',
    gap: spacing.md,
    minHeight: 44,
    paddingVertical: spacing.md,
  },
  optionLabel: {
    color: colors.text.primary,
    flex: 1,
  },
  optionLabelDanger: {
    color: colors.state.danger,
  },
  profileBlock: {
    alignItems: 'center',
    paddingTop: spacing.md,
  },
  rootedRow: {
    marginTop: spacing.sm,
  },
  // The options bottom sheet: rounded top corners, the canvas fill, and the
  // floating-bar shadow (elevation 3, whose offset points up).
  sheet: {
    ...elevation[3],
    backgroundColor: colors.bg.canvas,
    borderTopLeftRadius: radius.xxl,
    borderTopRightRadius: radius.xxl,
    paddingBottom: spacing.xl,
  },
  sheetBackdrop: {
    backgroundColor: colors.scrim.overlay,
    flex: 1,
    justifyContent: 'flex-end',
  },
  sheetBody: {
    paddingHorizontal: spacing.lg,
  },
  sheetHandle: {
    backgroundColor: colors.border.hairline,
    borderRadius: radius.pill,
    height: 4,
    width: 32,
  },
  sheetHandleArea: {
    alignItems: 'center',
    paddingBottom: spacing.sm,
    paddingTop: spacing.sm,
  },
  spinner: {
    marginVertical: spacing.xl,
  },
  stats: {
    color: colors.text.secondary,
    marginTop: spacing.sm,
  },
  tab: {
    borderBottomColor: 'transparent',
    borderBottomWidth: 2,
    paddingBottom: spacing.sm,
    paddingTop: spacing.md,
  },
  tabActive: {
    borderBottomColor: colors.brand.primary,
  },
  tabLabel: {
    color: colors.text.tertiary,
  },
  tabLabelActive: {
    color: colors.text.primary,
  },
  tabs: {
    borderBottomColor: colors.border.hairline,
    borderBottomWidth: 1,
    flexGrow: 0,
    marginHorizontal: -layout.screenPaddingFeed,
    marginTop: spacing.xl,
  },
  tabsContent: {
    gap: spacing.xl,
  },
});

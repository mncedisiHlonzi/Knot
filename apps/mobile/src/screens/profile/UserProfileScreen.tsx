import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Animated,
  FlatList,
  Image,
  Modal,
  PanResponder,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  View,
  type ImageLoadEventData,
  type NativeSyntheticEvent,
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
import { blocksApi, describeModerationError } from '../../api/moderation';
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
import FeedVideo from '../../components/FeedVideo';
import Header from '../../components/Header';
import MediaPickerSheet, { PickedMedia } from '../../components/MediaPickerSheet';
import PillarTag from '../../components/PillarTag';
import RootedBadge from '../../components/RootedBadge';
import { API_BASE_URL } from '../../config/api';
import {
  colors,
  elevation,
  fontWeights,
  layout,
  radius,
  spacing,
  tabularNums,
  typeScale,
} from '../../theme';
import { PLACEHOLDER_MEDIA_ASPECT_RATIO } from '../../utils/mediaFrame';
import { useMediaAspectRatio } from '../../utils/useMediaAspectRatio';
import { formatRelativeTime } from '../../utils/time';
import {
  DEFAULT_PROFILE_TAB,
  PROFILE_TABS,
  activityCard,
  filterActivities,
  profileStats,
  profileTabEmptyHeadline,
  statsLine,
  type ProfileCard,
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
  /** Called when the owner wants to see and manage the people they have blocked. */
  readonly onOpenBlockedUsers: () => void;
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
 * The card's media block: a still image, or a video's paused first frame, or
 * nothing when the card carries no media.
 *
 * The API sends a relative path, so it is joined to the base URL the way the
 * feed's media is. A video never plays here — the wall is a browse surface, and
 * playback belongs to the Feed (KNOT-020g-fix4).
 *
 * The frame is **full-bleed** (it escapes the card's gutter) and is the **media's
 * own shape**: full width, with a height that follows the media's natural
 * width:height ratio, so a portrait photo is tall and a landscape one is short. The
 * ratio comes from the payload when the server reported the item's dimensions, and
 * otherwise from the media itself, which reports its natural size on load — which
 * is the case that matters for video, since the picker reports no size for most
 * video assets (KNOT-020g-fix5, KNOT-020g-fix7). Until either answers, the frame is
 * a full-width square of `bg.sunken`; the media fills whatever frame it is given
 * (`cover`), so a placeholder is never a bar.
 *
 * It is a component rather than a helper that returns an element, because
 * resolving the shape from the loaded media needs state, and state cannot live in
 * a function a parent calls mid-render. It is local to this file for the same
 * reason `OptionRow` is: the profile wall is the only place that draws one
 * (KNOT-020g-fix7).
 */
function CardMedia({ card }: { readonly card: ProfileCard }): React.ReactElement | null {
  // The hook is called before the early return, so the component always calls it
  // however the card changes between renders.
  const { aspectRatio, onLoadSize } = useMediaAspectRatio(card.mediaWidth, card.mediaHeight);

  if (card.mediaUrl === undefined || card.mediaType === undefined) {
    return null;
  }

  const uri = `${API_BASE_URL}${card.mediaUrl}`;
  const frame = [styles.cardMedia, { aspectRatio: aspectRatio ?? PLACEHOLDER_MEDIA_ASPECT_RATIO }];

  /** Takes the shape off a decoded image, for media the payload could not size. */
  const handleImageLoad = (event: NativeSyntheticEvent<ImageLoadEventData>): void => {
    const { width, height } = event.nativeEvent.source;
    onLoadSize(width, height);
  };

  if (card.mediaType === 'video') {
    return (
      <View style={frame}>
        <FeedVideo uri={uri} previewOnly onLoadSize={onLoadSize} />
      </View>
    );
  }

  return (
    <Image
      source={{ uri }}
      style={frame}
      resizeMode="cover"
      onLoad={handleImageLoad}
      accessibilityIgnoresInvertColors
    />
  );
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
      <IconComponent
        color={danger ? colors.state.danger : colors.text.tertiary}
        size={20}
        weight="bold"
      />
      <Text
        style={[typeScale.body, styles.optionLabel, danger ? styles.optionLabelDanger : null]}
        numberOfLines={1}
      >
        {label}
      </Text>
      {busy ? (
        <ActivityIndicator size="small" color={colors.text.tertiary} />
      ) : (
        <CaretRight color={colors.text.tertiary} size={20} weight="bold" />
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
 * entry is a card: an overline (or the story's pillar tag), the title, the story's
 * first media item when it has one, and a meta line, closed by a full-bleed
 * hairline — the design reference's tab content.
 *
 * The wall shows only what the person authored themselves. Comments, bridges, and
 * replies to questions still arrive on the stream (the API is unchanged) but are
 * filtered out of the display and given no tab (KNOT-020g-fix).
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
  onOpenBlockedUsers,
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
          // A reply to a question has no screen of its own: it belongs to the
          // question it responds to, which is where the reader wants to land.
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
        centered
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
                  <PillarTag pillar={card.pillar} place={card.place} variant="plain" />
                </View>
              )}

              <Text style={[typeScale.h3, styles.cardTitle]} numberOfLines={2}>
                {card.title}
              </Text>

              {card.preview === '' ? null : (
                <Text style={[typeScale.bodyS, styles.cardPreview]} numberOfLines={2}>
                  {card.preview}
                </Text>
              )}

              <CardMedia card={card} />

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
                      onOpenBlockedUsers();
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
  // The card's media preview: full-bleed and never a player — a still image, or
  // a video's first frame (KNOT-020g-fix3, KNOT-020g-fix5). It escapes the card's
  // gutter so the media runs edge to edge, and its ratio is set per card from the
  // media's own dimensions, so it is not fixed here.
  cardMedia: {
    marginHorizontal: -layout.screenPaddingFeed,
    marginTop: spacing.sm,
    overflow: 'hidden',
    width: '100%',
  },
  cardOverline: {
    color: colors.brand.primary,
  },
  cardPillar: {
    alignSelf: 'flex-start',
  },
  // The card's body excerpt, under the title — the feed's reading order.
  cardPreview: {
    color: colors.text.secondary,
    marginTop: spacing.xs,
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
  // chevron. Rows are separated by spacing alone — no drawn dividers
  // (KNOT-020g-fix3).
  option: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: spacing.md,
    minHeight: 44,
    paddingVertical: spacing.md,
  },
  optionLabel: {
    color: colors.text.primary,
    flex: 1,
    // The brief asks for a heavier label than the body composite's 400; 700 is the
    // weight every other label in the app carries (KNOT-020g-fix2).
    fontWeight: fontWeights.bold,
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
    // Three natural-width tabs, centred as a group: `flexGrow: 1` lets the content
    // container fill the viewport so `justifyContent` has the space to centre
    // within, while a row wider than the screen still scrolls (KNOT-020g-fix4). The
    // row itself stays full-bleed, so its hairline spans the screen.
    flexGrow: 1,
    gap: spacing.xl,
    justifyContent: 'center',
    paddingHorizontal: layout.screenPaddingFeed,
  },
});

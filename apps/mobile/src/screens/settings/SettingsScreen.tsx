import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import {
  ArrowLeft,
  Camera,
  CaretRight,
  DeviceMobile,
  Devices,
  FileText,
  Info,
  Moon,
  Plant,
  Prohibit,
  ShieldCheck,
  Star,
  Sun,
  Trash,
  type Icon,
} from 'phosphor-react-native';

import { User, describeError } from '../../api/client';
import { type ProfileUser, profileApi } from '../../api/profile';
import { uploadAvatar } from '../../api/users';
import Avatar from '../../components/Avatar';
import Header from '../../components/Header';
import MediaPickerSheet, { type PickedMedia } from '../../components/MediaPickerSheet';
import { API_BASE_URL } from '../../config/api';
import { APP_VERSION } from '../../config/app';
import type { LegalDocument } from '../../data/legalDocuments';
import {
  fontWeights,
  layout,
  radius,
  spacing,
  typeScale,
  useTheme,
  useThemedStyles,
} from '../../theme';
import type { AppearanceMode, ThemeColors } from '../../theme';

/**
 * Resolves the profile's relative avatar path against the API base URL.
 *
 * The server sends `/users/{id}/avatar?v=…`, so it is joined to the base the way
 * every other avatar in the app is. A missing or blank path resolves to
 * `undefined`, which `Avatar` reads as "draw initials".
 */
function avatarUri(path: string | null | undefined): string | undefined {
  const trimmed = typeof path === 'string' ? path.trim() : '';
  return trimmed === '' ? undefined : `${API_BASE_URL}${trimmed}`;
}

type SettingsScreenProps = {
  /** The signed-in user's id, used to read their own profile header. */
  readonly userId: string;
  /**
   * The signed-in user's email address, drawn under their name in the header.
   *
   * It comes from the session rather than the profile endpoint: `ProfileUser` has
   * no email field, and the person reading this screen is by definition the owner
   * of the account (KNOT-020k-fix).
   */
  readonly email?: string;
  /** The signed-in user's access token, needed for the avatar upload. */
  readonly token: string;
  /** Called when the person returns to the profile. */
  readonly onBack: () => void;
  /** Called with the updated user after a successful avatar upload. */
  readonly onUserUpdated: (user: User) => void;
  /** Called when the owner wants to set or replace their Rooted signal. */
  readonly onSetRooted: () => void;
  /** Called when the owner wants to see and manage the people they have blocked. */
  readonly onOpenBlockedUsers: () => void;
  /** Called when the owner wants to see the devices they are signed in on. */
  readonly onOpenSessions: () => void;
  /** Called when the owner wants to delete their account. */
  readonly onOpenDeleteAccount: () => void;
  /** Called when the owner opens one of the legal documents (KNOT-035). */
  readonly onOpenLegal: (document: LegalDocument) => void;
  /** Called when the owner chooses to sign out. */
  readonly onSignOut: () => void;
};

/**
 * One tappable (or informational) row: a leading glyph, a label, an optional value
 * and a trailing chevron.
 *
 * A row with no `onPress` is drawn as a plain `View` and carries no chevron, so an
 * informational row (the app version) never looks like a control that does nothing.
 * `busy` swaps the chevron for a spinner while the row's action is in flight, the
 * same affordance the profile's old options sheet used (KNOT-020g-fix).
 *
 * `divider` draws the hairline that parts it from the row above, inside a bento
 * container. It is opt-in and never drawn on a group's first row, so the card's
 * own top edge is the only rule above it (KNOT-020k-fix).
 */
function SettingRow({
  icon,
  label,
  value,
  danger = false,
  busy = false,
  divider = false,
  onPress,
}: {
  readonly icon: Icon;
  readonly label: string;
  readonly value?: string;
  readonly danger?: boolean;
  readonly busy?: boolean;
  readonly divider?: boolean;
  readonly onPress?: () => void;
}): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const IconComponent = icon;
  const tint = danger ? colors.state.danger : colors.text.tertiary;
  const rowStyle = [styles.row, divider ? styles.rowDivider : null];

  const content = (
    <>
      {/* No explicit weight: the row glyphs inherit the app-wide weight. */}
      <IconComponent color={tint} size={20} />
      <Text
        style={[typeScale.body, styles.rowLabel, danger ? styles.rowLabelDanger : null]}
        numberOfLines={1}
      >
        {label}
      </Text>
      {value === undefined ? null : (
        <Text style={[typeScale.caption, styles.rowValue]} numberOfLines={1}>
          {value}
        </Text>
      )}
      {busy ? (
        <ActivityIndicator size="small" color={colors.text.tertiary} />
      ) : onPress === undefined ? null : (
        <CaretRight color={colors.text.tertiary} size={20} />
      )}
    </>
  );

  if (onPress === undefined) {
    return <View style={rowStyle}>{content}</View>;
  }

  return (
    <Pressable
      style={rowStyle}
      onPress={onPress}
      disabled={busy}
      accessibilityRole="button"
      accessibilityLabel={value === undefined ? label : `${label}, ${value}`}
      accessibilityState={{ busy, disabled: busy }}
    >
      {content}
    </Pressable>
  );
}

/**
 * The three appearance choices, in order.
 *
 * `System` is a real choice, not the absence of one: it follows the device and
 * keeps following it while the app runs. Choosing Light or Dark pins the mode and
 * persists it under `knot.appearance.v1`; the change applies on the tap, not on a
 * restart (KNOT-020k, KNOT-ADR-096). They are drawn as one horizontal row of
 * pills rather than stacked radio rows, which is the bento shape the founder
 * asked for (KNOT-020k-fix).
 */
const APPEARANCE_CHOICES: readonly {
  readonly mode: AppearanceMode;
  readonly label: string;
  readonly icon: Icon;
}[] = [
  { mode: 'system', label: 'System', icon: DeviceMobile },
  { mode: 'light', label: 'Light', icon: Sun },
  { mode: 'dark', label: 'Dark', icon: Moon },
];

/** The glyph size inside an appearance pill. */
const APPEARANCE_ICON_SIZE = 18;

/** The glyph disc on the Rooted status card. */
const STATUS_ICON_SIZE = 40;

/** The glyph inside that disc. */
const STATUS_GLYPH_SIZE = 20;

/** A group heading above one bento container. */
function SectionHeading({ title }: { readonly title: string }): React.ReactElement {
  const styles = useThemedStyles(createStyles);

  return <Text style={[typeScale.h3, styles.sectionTitle]}>{title}</Text>;
}

/**
 * Settings: the account and preference surface reached from the profile's
 * overflow control (KNOT-020k-fix22).
 *
 * It replaces the profile's options bottom sheet, which had grown into a menu of
 * unrelated things — an account action, a device preference and the way out of the
 * app, all behind one chevron. A pushed screen can say what each group is for, and
 * it leaves room for the sections the app actually needs: the account, the
 * preferences, the About block, and the two legal documents (KNOT-020k-fix22,
 * KNOT-035).
 *
 * The avatar flow lives here now, because "Edit avatar" is an account action rather
 * than a profile-card action. The upload reports through `onUserUpdated`, which
 * writes the new user back to the session, so the wall behind this screen shows the
 * new face as soon as it is read again.
 *
 * The structure is the design system's: a canvas, `typeScale` composites,
 * semantic colour roles and 44-point minimum touch targets. No hex is written
 * here.
 *
 * The layout is the founder's **bento** treatment (KNOT-020k-fix): a centred
 * profile block (avatar, name, email), a full-width Rooted status card, one
 * rounded container per group with hairlines between its rows, a horizontal
 * Appearance pill row, and a red text-only Sign out below every container. The
 * containers are `bg.raised` with a hairline edge and a `radius.xxl` corner, so a
 * group reads as one object rather than as a list of loose rows — and in light
 * mode, where `bg.raised` is the canvas value, that edge is what separates the
 * card from the page.
 *
 * The block at the top is the signed-in person's own header — avatar, name and
 * email — read from the same profile endpoint the wall uses, so Settings opens
 * with the face it belongs to. It is enrichment: if that read fails the screen
 * still works, it just draws no header. The Rooted summary moved into the status
 * card, where the reference puts it.
 */
export default function SettingsScreen({
  userId,
  email = '',
  token,
  onBack,
  onUserUpdated,
  onSetRooted,
  onOpenBlockedUsers,
  onOpenSessions,
  onOpenDeleteAccount,
  onOpenLegal,
  onSignOut,
}: SettingsScreenProps): React.ReactElement {
  const { colors, mode, setMode } = useTheme();
  const styles = useThemedStyles(createStyles);
  // Declared first so the header's profile is the screen's first piece of state.
  const [profile, setProfile] = useState<ProfileUser | undefined>(undefined);
  const [pickerVisible, setPickerVisible] = useState(false);
  const [uploadingAvatar, setUploadingAvatar] = useState(false);
  const [avatarError, setAvatarError] = useState<string | undefined>(undefined);

  /**
   * Reads the signed-in person's profile for the header.
   *
   * One activity is asked for and never shown: the endpoint returns the user
   * beside the wall, so the smallest possible page carries the header's three
   * fields without a second route. A failure is swallowed on purpose — the header
   * is decoration, and the settings below it do not depend on it.
   */
  const loadProfile = useCallback(async (): Promise<void> => {
    try {
      const page = await profileApi.getUserProfile(userId, undefined, 1, token);
      setProfile(page.user);
    } catch {
      setProfile(undefined);
    }
  }, [token, userId]);

  useEffect(() => {
    void loadProfile();
  }, [loadProfile]);

  const handleAvatarPicked = useCallback(
    async (media: PickedMedia): Promise<void> => {
      setAvatarError(undefined);
      setUploadingAvatar(true);
      try {
        const updated = await uploadAvatar(media.uri, media.mimeType, token);
        onUserUpdated(updated);
        // The header shows the same avatar, so it is re-read rather than left
        // showing the face from before the upload (KNOT-020k-fix23).
        void loadProfile();
      } catch (caught) {
        setAvatarError(describeError(caught));
      } finally {
        setUploadingAvatar(false);
      }
    },
    [loadProfile, onUserUpdated, token],
  );

  // The person's Rooted summary, or `null` when they have none — the status card
  // says which of the two states it is drawing.
  const rooted = profile?.rooted ?? null;

  return (
    <View style={styles.container}>
      <Header
        title="Settings"
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

      <ScrollView contentContainerStyle={styles.content}>
        {/* The signed-in person's own header: the face, the name and the
            address, centred (KNOT-020k-fix). Nothing here is a control. */}
        {profile === undefined ? null : (
          <View style={styles.profileHeader}>
            {/* `Avatar` pins itself left (`alignSelf: 'flex-start'`) so it never
                stretches in a row; in this centred column that would defeat the
                header's alignment, so it gets a centring wrapper — the same
                device the Profile wall uses (KNOT-020k-fix26). */}
            <View style={styles.profileAvatar}>
              <Avatar
                uri={avatarUri(profile.avatar_url)}
                displayName={profile.display_name}
                size="lg"
              />
            </View>
            <Text style={[typeScale.h2, styles.profileName]} numberOfLines={1}>
              {profile.display_name}
            </Text>
            {email === '' ? null : (
              <Text style={[typeScale.caption, styles.profileEmail]} numberOfLines={1}>
                {email}
              </Text>
            )}
          </View>
        )}

        {/* The status card: the person's Rooted standing, or the way to set it.
            The whole card is the target — the pill on the right is its affordance,
            not a second control (KNOT-020k-fix). */}
        <Pressable
          style={styles.statusCard}
          onPress={onSetRooted}
          accessibilityRole="button"
          accessibilityLabel={rooted === null ? 'Set your Rooted place' : 'View your Rooted place'}
        >
          <View style={styles.statusIcon}>
            <Star color={colors.text.onBrand} size={STATUS_GLYPH_SIZE} weight="fill" />
          </View>
          <View style={styles.statusText}>
            <Text style={[typeScale.h3, styles.statusTitle]} numberOfLines={1}>
              {rooted === null ? 'Become Rooted' : 'Rooted Status'}
            </Text>
            <Text style={[typeScale.caption, styles.statusSubtitle]} numberOfLines={1}>
              {rooted === null ? 'Unlock advanced features' : `Rooted in ${rooted.place}`}
            </Text>
          </View>
          <View style={styles.statusPill}>
            <Text style={[typeScale.label, styles.statusPillLabel]}>
              {rooted === null ? 'Set up' : 'View'}
            </Text>
          </View>
        </Pressable>

        <SectionHeading title="Appearance" />
        <View style={[styles.card, styles.cardBody]}>
          <View style={styles.pillRow}>
            {APPEARANCE_CHOICES.map((choice) => {
              const selected = mode === choice.mode;
              const AppearanceIcon = choice.icon;
              return (
                <Pressable
                  key={choice.mode}
                  style={[styles.pill, selected ? styles.pillActive : styles.pillIdle]}
                  onPress={() => setMode(choice.mode)}
                  accessibilityRole="radio"
                  accessibilityLabel={choice.label}
                  accessibilityState={{ selected }}
                >
                  <AppearanceIcon
                    color={selected ? colors.text.inverse : colors.text.primary}
                    size={APPEARANCE_ICON_SIZE}
                  />
                  <Text
                    style={[
                      typeScale.label,
                      selected ? styles.pillLabelActive : styles.pillLabelIdle,
                    ]}
                    numberOfLines={1}
                  >
                    {choice.label}
                  </Text>
                </Pressable>
              );
            })}
          </View>
        </View>

        <SectionHeading title="Account" />
        <View style={styles.card}>
          <SettingRow
            icon={Camera}
            label="Edit avatar"
            busy={uploadingAvatar}
            onPress={() => {
              setAvatarError(undefined);
              setPickerVisible(true);
            }}
          />
          <SettingRow divider icon={Plant} label="Set Rooted" onPress={onSetRooted} />
          {/* The device list (KNOT-ADR-084): where a person reviews and ends the
              sessions their account is signed in on. */}
          <SettingRow divider icon={Devices} label="Sessions" onPress={onOpenSessions} />
          {/* Destructive, and last in the group: it is the one row here that
              cannot be undone from this screen (KNOT-034). */}
          <SettingRow
            divider
            icon={Trash}
            label="Delete account"
            danger
            onPress={onOpenDeleteAccount}
          />
        </View>

        {avatarError === undefined ? null : (
          <Text style={[typeScale.bodyS, styles.error]}>{avatarError}</Text>
        )}

        <SectionHeading title="Privacy" />
        <View style={styles.card}>
          <SettingRow icon={Prohibit} label="Blocked users" onPress={onOpenBlockedUsers} />
        </View>

        <SectionHeading title="About" />
        <View style={styles.card}>
          <SettingRow icon={Info} label="App version" value={`v${APP_VERSION}`} />
          {/* The documents are shipped with the app and read in place, so nothing
              leaves Knot to read them (KNOT-035). */}
          <SettingRow
            divider
            icon={ShieldCheck}
            label="Privacy Policy"
            onPress={() => onOpenLegal('privacy')}
          />
          <SettingRow
            divider
            icon={FileText}
            label="Terms of Service"
            onPress={() => onOpenLegal('terms')}
          />
        </View>

        {/* Sign out is an action on the session, not a setting, so it stands
            outside every container as a quiet red line (KNOT-020k-fix). */}
        <Pressable
          style={styles.signOut}
          onPress={onSignOut}
          accessibilityRole="button"
          accessibilityLabel="Sign out"
        >
          <Text style={[typeScale.button, styles.signOutLabel]}>Sign out</Text>
        </Pressable>
      </ScrollView>

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
      />
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
    },
    error: {
      color: colors.state.danger,
      paddingHorizontal: layout.screenPaddingFeed,
      marginTop: spacing.md,
    },
    // A 44-point tap target for the header's back control.
    iconButton: {
      alignItems: 'center',
      height: 44,
      justifyContent: 'center',
      width: 44,
    },
    // One row: a leading glyph, the label, an optional value, and the trailing
    // control. Rows sit inside a bento container, parted by a hairline — never a
    // rule on the first row (KNOT-020k-fix).
    row: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.md,
      minHeight: 44,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.md,
    },
    // The hairline that parts one row from the row above it, inside a container.
    rowDivider: {
      borderTopColor: colors.border.hairline,
      borderTopWidth: 1,
    },
    // The label takes the body composite at the bold weight: a settings row is a
    // control, so it reads decisively without becoming a heading
    // (KNOT-020k-fix24).
    rowLabel: {
      color: colors.text.primary,
      flex: 1,
      fontWeight: fontWeights.bold,
    },
    rowLabelDanger: {
      color: colors.state.danger,
    },
    rowValue: {
      color: colors.text.tertiary,
    },
    // The header is a centred column: a 48pt face, the name beneath it, then the
    // address (KNOT-020k-fix).
    profileHeader: {
      alignItems: 'center',
      paddingHorizontal: layout.screenPaddingFeed,
      paddingBottom: spacing.md,
      paddingTop: spacing.lg,
    },
    // `Avatar` positions itself (`alignSelf: 'flex-start'`), so it needs a box that
    // shrinks to its content and is then centred. A *stretched* wrapper would not
    // help: the child's own `alignSelf` wins inside it and the face would hug the
    // left edge again (KNOT-020k-fix26).
    profileAvatar: {
      alignSelf: 'center',
    },
    profileName: {
      color: colors.text.primary,
      marginTop: spacing.md,
      textAlign: 'center',
    },
    profileEmail: {
      color: colors.text.tertiary,
      marginTop: spacing.xs,
      textAlign: 'center',
    },
    // The Rooted status card: a full-width raised surface with a hairline edge, the
    // glyph disc on the left, the copy in the middle and the pill on the right
    // (KNOT-020k-fix).
    statusCard: {
      alignItems: 'center',
      backgroundColor: colors.bg.raised,
      borderColor: colors.border.hairline,
      borderRadius: radius.xxl,
      borderWidth: 1,
      flexDirection: 'row',
      gap: spacing.md,
      marginHorizontal: layout.screenPaddingFeed,
      padding: spacing.lg,
    },
    // The glyph sits on a solid disc so the card opens with weight: a bare glyph
    // would read as one more row icon.
    statusIcon: {
      alignItems: 'center',
      backgroundColor: colors.brand.primary,
      borderRadius: radius.pill,
      height: STATUS_ICON_SIZE,
      justifyContent: 'center',
      width: STATUS_ICON_SIZE,
    },
    statusText: {
      flex: 1,
      gap: spacing.xs / 2,
    },
    statusTitle: {
      color: colors.text.primary,
    },
    statusSubtitle: {
      color: colors.text.tertiary,
    },
    // The pill is the card's affordance, not a second control: the whole card is
    // the tap target, so this is drawn as a quiet sunken chip rather than a filled
    // button (KNOT-020k-fix).
    statusPill: {
      alignItems: 'center',
      backgroundColor: colors.bg.sunken,
      borderRadius: radius.pill,
      justifyContent: 'center',
      minHeight: 32,
      paddingHorizontal: spacing.md,
    },
    statusPillLabel: {
      color: colors.text.primary,
    },
    // The group heading: an h3 in the primary ink, on the same gutter as the card it
    // names (KNOT-020k-fix).
    sectionTitle: {
      color: colors.text.primary,
      marginBottom: spacing.sm,
      marginHorizontal: layout.screenPaddingFeed,
      marginTop: spacing.xl,
    },
    // One group of rows: a raised surface with a hairline edge and a large corner,
    // so the group reads as one object. `overflow: 'hidden'` keeps a row's hairline
    // and pressed fill inside the rounded edge (KNOT-020k-fix).
    card: {
      backgroundColor: colors.bg.raised,
      borderColor: colors.border.hairline,
      borderRadius: radius.xxl,
      borderWidth: 1,
      marginHorizontal: layout.screenPaddingFeed,
      overflow: 'hidden',
    },
    // The Appearance container holds a single row of pills, so it is padded rather
    // than rowed.
    cardBody: {
      padding: spacing.sm,
    },
    pillRow: {
      flexDirection: 'row',
      gap: spacing.sm,
    },
    // One appearance choice: an equal third of the row, pill shaped and 44 points
    // tall, with the glyph beside the name.
    pill: {
      alignItems: 'center',
      borderRadius: radius.pill,
      flex: 1,
      flexDirection: 'row',
      gap: spacing.xs,
      justifyContent: 'center',
      minHeight: 44,
      paddingHorizontal: spacing.sm,
    },
    // The selected pill is the primary fill with inverse ink; the rest are sunken
    // with primary ink. This is the brief's bg.primary/text.inverse pair, named
    // through the monochrome roles (KNOT-020k-fix).
    pillActive: {
      backgroundColor: colors.brand.primary,
    },
    pillIdle: {
      backgroundColor: colors.bg.sunken,
    },
    pillLabelActive: {
      color: colors.text.inverse,
    },
    pillLabelIdle: {
      color: colors.text.primary,
    },
    // Sign out stands outside every container and apart from them: it is an action
    // on the session, not a setting (KNOT-020k-fix).
    signOut: {
      alignItems: 'center',
      justifyContent: 'center',
      marginHorizontal: layout.screenPaddingFeed,
      marginTop: spacing.xl,
      minHeight: 44,
    },
    signOutLabel: {
      color: colors.state.danger,
      textAlign: 'center',
    },
  });

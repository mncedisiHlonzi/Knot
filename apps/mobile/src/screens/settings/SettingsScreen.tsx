import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Linking,
  Pressable,
  ScrollView,
  StyleSheet,
  Switch,
  Text,
  View,
} from 'react-native';
import {
  ArrowLeft,
  Camera,
  CaretRight,
  FileText,
  Info,
  Moon,
  Plant,
  Prohibit,
  ShieldCheck,
  SignOut,
  type Icon,
} from 'phosphor-react-native';

import { User, describeError } from '../../api/client';
import { type ProfileUser, profileApi } from '../../api/profile';
import { uploadAvatar } from '../../api/users';
import Avatar from '../../components/Avatar';
import Header from '../../components/Header';
import MediaPickerSheet, { type PickedMedia } from '../../components/MediaPickerSheet';
import RootedBadge from '../../components/RootedBadge';
import { API_BASE_URL } from '../../config/api';
import { APP_VERSION } from '../../config/app';
import { fontWeights, layout, spacing, typeScale, useTheme, useThemedStyles } from '../../theme';
import type { ThemeColors } from '../../theme';

/**
 * Where the legal pages will live once they are published.
 *
 * They are placeholders on the reserved `.example` domain on purpose: the app must
 * not imply a page exists before it does, and a broken open is reported rather than
 * swallowed (KNOT-020k-fix22).
 */
const PRIVACY_URL = 'https://knot.example/privacy';
const TERMS_URL = 'https://knot.example/terms';

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
 */
function SettingRow({
  icon,
  label,
  value,
  danger = false,
  busy = false,
  onPress,
}: {
  readonly icon: Icon;
  readonly label: string;
  readonly value?: string;
  readonly danger?: boolean;
  readonly busy?: boolean;
  readonly onPress?: () => void;
}): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const IconComponent = icon;
  const tint = danger ? colors.state.danger : colors.text.tertiary;

  const content = (
    <>
      <IconComponent color={tint} size={20} weight="bold" />
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
        <CaretRight color={colors.text.tertiary} size={20} weight="bold" />
      )}
    </>
  );

  if (onPress === undefined) {
    return <View style={styles.row}>{content}</View>;
  }

  return (
    <Pressable
      style={styles.row}
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
 * The theme preference: the current value and the switch that changes it.
 *
 * The switch is the control (it carries the accessible name and state); the row is
 * not separately pressable, so a tap on the label does nothing rather than
 * something ambiguous. The choice is a device preference stored outside the
 * account, so it applies at once and survives a restart (KNOT-ADR-077).
 *
 * There are two states — light and dark. The app follows the system appearance on
 * first launch and then remembers an explicit choice, so "System" is the initial
 * behaviour rather than a third value the switch can return to
 * (KNOT-020k-fix22).
 */
function ThemeRow(): React.ReactElement {
  const { colors, theme, setTheme } = useTheme();
  const styles = useThemedStyles(createStyles);
  const isDark = theme === 'dark';
  const valueLabel = isDark ? 'Dark' : 'Light';

  return (
    <View style={styles.row}>
      <Moon color={colors.text.tertiary} size={20} weight="bold" />
      <Text style={[typeScale.body, styles.rowLabel]} numberOfLines={1}>
        Theme
      </Text>
      <Text style={[typeScale.caption, styles.rowValue]}>{valueLabel}</Text>
      <Switch
        value={isDark}
        onValueChange={(next) => setTheme(next ? 'dark' : 'light')}
        trackColor={{ false: colors.bg.sunken, true: colors.brand.primary }}
        thumbColor={colors.bg.canvas}
        ios_backgroundColor={colors.bg.sunken}
        accessibilityRole="switch"
        accessibilityLabel="Theme"
        accessibilityValue={{ text: valueLabel }}
      />
    </View>
  );
}

/** A titled group of rows: an overline heading above one continuous column. */
function Section({
  title,
  children,
}: {
  readonly title: string;
  readonly children: React.ReactNode;
}): React.ReactElement {
  const styles = useThemedStyles(createStyles);

  return (
    <View style={styles.section}>
      <Text style={[typeScale.overline, styles.sectionTitle]}>{title}</Text>
      <View style={styles.sectionBody}>{children}</View>
    </View>
  );
}

/**
 * Settings: the account and preference surface reached from the profile's
 * overflow control (KNOT-020k-fix22).
 *
 * It replaces the profile's options bottom sheet, which had grown into a menu of
 * unrelated things — an account action, a device preference and the way out of the
 * app, all behind one chevron. A pushed screen can say what each group is for, and
 * it leaves room for the sections the app actually needs: the account, the
 * preferences, and the About block (version, privacy, terms).
 *
 * The avatar flow lives here now, because "Edit avatar" is an account action rather
 * than a profile-card action. The upload reports through `onUserUpdated`, which
 * writes the new user back to the session, so the wall behind this screen shows the
 * new face as soon as it is read again.
 *
 * The structure is the design system's: a canvas, `typeScale` composites,
 * semantic colour roles and 44-point minimum touch targets. No hex is written
 * here, and no row is carded or ruled — the groups are set apart by whitespace and
 * an overline heading, the way the rest of the app draws a quiet list
 * (KNOT-020k-fix23).
 *
 * The block at the top is the signed-in person's own header — avatar, name and
 * Rooted summary — read from the same profile endpoint the wall uses, so Settings
 * opens with the face it belongs to. It is enrichment: if that read fails the
 * screen still works, it just draws no header.
 */
export default function SettingsScreen({
  userId,
  token,
  onBack,
  onUserUpdated,
  onSetRooted,
  onOpenBlockedUsers,
  onSignOut,
}: SettingsScreenProps): React.ReactElement {
  const { colors } = useTheme();
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

  /** Opens a legal page, reporting a link the device cannot follow. */
  const openExternal = useCallback((url: string): void => {
    void Linking.openURL(url).catch(() => {
      Alert.alert('Link unavailable', 'This page has not been published yet.');
    });
  }, []);

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
        {/* The signed-in person's own header, as a left-aligned profile row: the
            face on the left, the name and the Rooted summary stacked beside it, so
            the block reads as a card rather than as a centred badge over a list
            (KNOT-020k-fix24). Nothing here is a control. */}
        {profile === undefined ? null : (
          <View style={styles.profileHeader}>
            <Avatar
              uri={avatarUri(profile.avatar_url)}
              displayName={profile.display_name}
              size="lg"
            />
            <View style={styles.profileIdentity}>
              <Text style={[typeScale.h2, styles.profileName]} numberOfLines={1}>
                {profile.display_name}
              </Text>
              {profile.rooted === null ? null : (
                <View style={styles.profileRooted}>
                  <RootedBadge
                    place={profile.rooted.place}
                    durationBucket={profile.rooted.duration_bucket}
                  />
                </View>
              )}
            </View>
          </View>
        )}

        <Section title="Account">
          <SettingRow
            icon={Camera}
            label="Edit avatar"
            busy={uploadingAvatar}
            onPress={() => {
              setAvatarError(undefined);
              setPickerVisible(true);
            }}
          />
          <SettingRow icon={Plant} label="Set Rooted" onPress={onSetRooted} />
          <SettingRow icon={Prohibit} label="Blocked users" onPress={onOpenBlockedUsers} />
        </Section>

        {avatarError === undefined ? null : (
          <Text style={[typeScale.bodyS, styles.error]}>{avatarError}</Text>
        )}

        <Section title="Preferences">
          <ThemeRow />
        </Section>

        <Section title="About">
          <SettingRow icon={Info} label="App version" value={`v${APP_VERSION}`} />
          <SettingRow
            icon={ShieldCheck}
            label="Privacy Policy"
            onPress={() => openExternal(PRIVACY_URL)}
          />
          <SettingRow
            icon={FileText}
            label="Terms of Use"
            onPress={() => openExternal(TERMS_URL)}
          />
        </Section>

        <View style={styles.signOut}>
          <SettingRow icon={SignOut} label="Sign out" danger onPress={onSignOut} />
        </View>
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
    // control. Rows carry NO rule: the groups are told apart by whitespace and the
    // overline heading above them, which is the quieter, more modern list the
    // founder asked for (KNOT-020k-fix23).
    row: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.md,
      minHeight: 44,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.md,
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
    // The header is a left-aligned profile row: the face on the left, the name and
    // the Rooted summary stacked beside it (KNOT-020k-fix24). A 48pt avatar keeps
    // the row the height of two text lines instead of towering over them.
    profileHeader: {
      alignItems: 'center',
      flexDirection: 'row',
      gap: spacing.md,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.lg,
    },
    profileIdentity: {
      flexShrink: 1,
    },
    profileName: {
      color: colors.text.primary,
    },
    profileRooted: {
      alignSelf: 'flex-start',
      marginTop: spacing.xs,
    },
    section: {
      marginTop: spacing.xl,
    },
    sectionBody: {
      marginTop: spacing.sm,
    },
    // The group heading: the design system's overline, in the tertiary ink, on the
    // same gutter as the rows it names.
    sectionTitle: {
      color: colors.text.tertiary,
      paddingHorizontal: layout.screenPaddingFeed,
    },
    // Sign out stands apart from the groups above it: it is an action on the
    // session, not a setting.
    signOut: {
      marginTop: spacing.xl,
    },
  });

import React, { useCallback, useState } from 'react';
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
import { uploadAvatar } from '../../api/users';
import Header from '../../components/Header';
import MediaPickerSheet, { type PickedMedia } from '../../components/MediaPickerSheet';
import { APP_VERSION } from '../../config/app';
import { layout, spacing, typeScale, useTheme, useThemedStyles } from '../../theme';
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

type SettingsScreenProps = {
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
 * The structure is the design system's: a canvas, full-bleed hairlines between rows,
 * `typeScale` composites, semantic colour roles and 44-point minimum touch targets.
 * No hex is written here, and no row is carded — the list is one continuous column
 * the way the feed's entries are.
 */
export default function SettingsScreen({
  token,
  onBack,
  onUserUpdated,
  onSetRooted,
  onOpenBlockedUsers,
  onSignOut,
}: SettingsScreenProps): React.ReactElement {
  const { colors } = useTheme();
  const styles = useThemedStyles(createStyles);
  const [pickerVisible, setPickerVisible] = useState(false);
  const [uploadingAvatar, setUploadingAvatar] = useState(false);
  const [avatarError, setAvatarError] = useState<string | undefined>(undefined);

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
    // control. Rows are told apart by a full-bleed hairline above each one, so a
    // group reads as one continuous column with no carded boxes (the design
    // system's list rule).
    row: {
      alignItems: 'center',
      borderTopColor: colors.border.hairline,
      borderTopWidth: 1,
      flexDirection: 'row',
      gap: spacing.md,
      minHeight: 44,
      paddingHorizontal: layout.screenPaddingFeed,
      paddingVertical: spacing.md,
    },
    rowLabel: {
      color: colors.text.primary,
      flex: 1,
    },
    rowLabelDanger: {
      color: colors.state.danger,
    },
    rowValue: {
      color: colors.text.tertiary,
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

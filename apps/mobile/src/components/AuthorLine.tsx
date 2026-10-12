import React from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { API_BASE_URL } from '../config/api';
import { colors, spacing, typeScale } from '../theme';
import { formatRelativeTime } from '../utils/time';
import Avatar from './Avatar';
import RootedBadge from './RootedBadge';

/** The three sizes AuthorLine offers. */
export type AuthorLineSize = 'small' | 'medium' | 'large';

/**
 * The inline Rooted summary AuthorLine renders. It is typed as plain strings (not
 * the API's `AuthorRooted`) on purpose: the response types describe an optimistic
 * wire format, so the component takes the loosest shape its caller can supply and
 * validates the values at runtime.
 */
export type AuthorRootedSummary = {
  readonly place: string;
  readonly duration_bucket: string;
};

type AuthorLineProps = {
  /**
   * The author's display name. Optional and nullable: the API types describe the
   * wire format optimistically, and a response from an older server can omit it.
   * A missing or blank name omits the name segment.
   */
  readonly displayName?: string | null;
  /** The backend avatar path (`/users/{id}/avatar`), or null/empty for none. */
  readonly avatarUrl?: string | null;
  /** The content's ISO-8601 timestamp, rendered relative to now. */
  readonly createdAt?: string | null;
  /** The author's inline Rooted summary, or null when they have none. */
  readonly rooted?: AuthorRootedSummary | null;
  /** Controls the avatar diameter and text weight. Defaults to "small". */
  readonly size?: AuthorLineSize;
  /**
   * Move the Rooted summary onto its own quieter second line, under the name and
   * time, instead of sitting inline beside them (KNOT-020c-fix2).
   *
   * Defaults to `false`, which keeps the inline attribution every other screen
   * already renders (Story Detail, Comment Thread, the Language Tree). The Feed
   * passes `true`.
   */
  readonly showRootedOnNewLine?: boolean;
  /**
   * Called when the author's avatar or name is tapped, to open their profile.
   *
   * When omitted, the identity is plain (not a tap target). The Rooted badge and
   * the timestamp are never part of the tap target, so a caller can wrap the whole
   * line in a card that opens something else.
   */
  readonly onPress?: () => void;
  /** Test identifier. */
  readonly testID?: string;
};

/**
 * The Avatar size that pairs with each AuthorLine size. The two scales agree on
 * their diameters (24 / 32 / 48), so the mapping is by name only.
 */
const AVATAR_SIZE: Readonly<Record<AuthorLineSize, 'sm' | 'md' | 'lg'>> = {
  small: 'sm',
  medium: 'md',
  large: 'lg',
};

/**
 * One author's attribution on a single line: avatar, name, optional Rooted badge,
 * and the content's time as a relative phrase.
 *
 * It is the one place attribution is rendered, so a story, a version, a comment,
 * and a bridge all read the same way. Every segment is optional: a missing name or
 * timestamp is omitted rather than shown empty, and an author with no avatar gets
 * their initials in a circle instead (drawn by `Avatar`).
 *
 * Every field is validated as a string before it is used. The API response types
 * describe an optimistic wire format, so a response that omits an author field
 * delivers `undefined` at runtime; reading it must not crash the screen
 * (KNOT-015b-fix). A line with nothing at all to show renders `null` rather than an
 * empty row.
 *
 * When `onPress` is given, the avatar and name become a tap target for the
 * author's profile: a nested `Pressable` takes the touch itself, so the parent
 * card's own onPress does not fire for it (KNOT-ADR-043).
 */
export default function AuthorLine({
  displayName,
  avatarUrl,
  createdAt,
  rooted,
  size = 'small',
  showRootedOnNewLine = false,
  onPress,
  testID,
}: AuthorLineProps): React.ReactElement | null {
  const name = typeof displayName === 'string' ? displayName.trim() : '';

  const path = typeof avatarUrl === 'string' ? avatarUrl.trim() : '';
  const avatarUri = path === '' ? undefined : `${API_BASE_URL}${path}`;

  const relative =
    typeof createdAt === 'string' && createdAt !== '' ? formatRelativeTime(createdAt) : '';

  const hasRootedObject = rooted !== null && rooted !== undefined;
  const rootedPlace =
    hasRootedObject && typeof rooted.place === 'string' ? rooted.place.trim() : '';
  const durationBucket =
    hasRootedObject && typeof rooted.duration_bucket === 'string' ? rooted.duration_bucket : '';
  const hasRooted = rootedPlace !== '';

  // Nothing to show: render no row at all rather than an empty one. A row that
  // carries only a Rooted badge is still something, so it renders.
  if (avatarUri === undefined && name === '' && relative === '' && !hasRooted) {
    return null;
  }

  // The Feed's two-line form: the Rooted summary leaves the identity line and
  // becomes a quiet line of its own beneath it, so the name and time read as one
  // clean line (KNOT-020c-fix2).
  const rootedOnNewLine = showRootedOnNewLine === true && hasRooted;

  // The separator before the timestamp belongs only when something precedes it on
  // its line, and the Rooted badge no longer does when it has moved down.
  const hasLeadingSegment = name !== '' || (!rootedOnNewLine && hasRooted);

  const identity = (
    <>
      <Avatar
        uri={avatarUri}
        displayName={typeof displayName === 'string' ? displayName : ''}
        size={AVATAR_SIZE[size]}
        accessibilityLabel={name === '' ? 'Profile picture' : `${name}'s profile picture`}
      />
      {name !== '' ? (
        <Text
          style={[size === 'large' ? typeScale.h3 : typeScale.label, styles.name]}
          numberOfLines={1}
        >
          {name}
        </Text>
      ) : null}
    </>
  );

  const identityTarget =
    onPress !== undefined ? (
      <Pressable
        style={styles.identity}
        onPress={onPress}
        accessibilityRole="button"
        accessibilityLabel={name === '' ? 'Open profile' : `Open ${name}'s profile`}
      >
        {identity}
      </Pressable>
    ) : (
      <View style={styles.identity}>{identity}</View>
    );

  const timeText =
    relative === '' ? null : (
      <Text style={[typeScale.caption, styles.time]}>
        {hasLeadingSegment ? `· ${relative}` : relative}
      </Text>
    );

  if (rootedOnNewLine) {
    return (
      <View style={styles.stack} testID={testID}>
        <View style={styles.row}>
          {identityTarget}
          {timeText}
        </View>
        <RootedBadge place={rootedPlace} durationBucket={durationBucket} variant="quiet" />
      </View>
    );
  }

  return (
    <View style={styles.row} testID={testID}>
      {identityTarget}

      {hasRooted ? (
        <RootedBadge
          place={rootedPlace}
          durationBucket={durationBucket}
          compact={size === 'small'}
        />
      ) : null}

      {timeText}
    </View>
  );
}

const styles = StyleSheet.create({
  identity: {
    alignItems: 'center',
    flexDirection: 'row',
    flexShrink: 1,
    gap: spacing.sm,
  },
  name: {
    color: colors.text.primary,
    flexShrink: 1,
  },
  row: {
    alignItems: 'center',
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
  },
  stack: {
    gap: spacing.xs,
  },
  time: {
    color: colors.text.tertiary,
  },
});

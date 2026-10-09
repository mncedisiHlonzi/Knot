import React from 'react';
import { Image, Pressable, StyleSheet, Text, View } from 'react-native';

import type { AuthorRooted } from '../api/rooted';
import { API_BASE_URL } from '../config/api';
import { colors, fontSizes, fontWeights, radius, spacing } from '../theme';
import { getInitials } from '../utils/initials';
import { formatRelativeTime } from '../utils/time';
import RootedBadge from './RootedBadge';

/** The three sizes AuthorLine offers. */
export type AuthorLineSize = 'small' | 'medium' | 'large';

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
  readonly rooted?: AuthorRooted | null;
  /** Controls the avatar diameter and text weight. Defaults to "small". */
  readonly size?: AuthorLineSize;
  /**
   * Called when the author's avatar or name is tapped, to open their profile.
   *
   * When omitted, the identity is plain (not a tap target). The Rooted badge and
   * the timestamp are never part of the tap target, so a caller can wrap the whole
   * line in a card that opens something else.
   */
  readonly onPress?: () => void;
};

/** Avatar diameter per size, in points. */
const AVATAR_DIAMETER: Readonly<Record<AuthorLineSize, number>> = {
  small: 24,
  medium: 32,
  large: 48,
};

/** Initials font size per size, in points. */
const INITIALS_SIZE: Readonly<Record<AuthorLineSize, number>> = {
  small: fontSizes.xs,
  medium: fontSizes.sm,
  large: fontSizes.base,
};

/** Name font size per size, in points. */
const NAME_SIZE: Readonly<Record<AuthorLineSize, number>> = {
  small: fontSizes.sm,
  medium: fontSizes.base,
  large: fontSizes.md,
};

/** Timestamp font size per size, in points. */
const TIME_SIZE: Readonly<Record<AuthorLineSize, number>> = {
  small: fontSizes.xs,
  medium: fontSizes.sm,
  large: fontSizes.sm,
};

/**
 * One author's attribution on a single line: avatar, name, optional Rooted badge,
 * and the content's time as a relative phrase.
 *
 * It is the one place attribution is rendered, so a story, a version, a comment,
 * and a bridge all read the same way. Every segment is optional: a missing name or
 * timestamp is omitted rather than shown empty, and an author with no avatar gets
 * their initials in a coloured circle instead.
 *
 * Every field is validated as a string before it is used. The API response types
 * describe an optimistic wire format, so a response that omits an author field
 * delivers `undefined` at runtime; reading it must not crash the screen
 * (KNOT-015b-fix).
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
  onPress,
}: AuthorLineProps): React.ReactElement | null {
  const name = typeof displayName === 'string' ? displayName.trim() : '';
  const diameter = AVATAR_DIAMETER[size];

  const path = typeof avatarUrl === 'string' ? avatarUrl.trim() : '';
  const avatarUri = path === '' ? undefined : `${API_BASE_URL}${path}`;

  const relative =
    typeof createdAt === 'string' && createdAt !== '' ? formatRelativeTime(createdAt) : '';

  const hasRooted =
    rooted !== null &&
    rooted !== undefined &&
    typeof rooted.place === 'string' &&
    rooted.place.trim() !== '';

  // Nothing to show: render no row at all rather than an empty one. A row that
  // carries only a Rooted badge is still something, so it renders.
  if (avatarUri === undefined && name === '' && relative === '' && !hasRooted) {
    return null;
  }

  const hasLeadingSegment = name !== '' || hasRooted;

  const avatar =
    avatarUri === undefined ? (
      <View style={[styles.avatarFallback, { height: diameter, width: diameter }]}>
        <Text style={[styles.initials, { fontSize: INITIALS_SIZE[size] }]}>
          {getInitials(displayName)}
        </Text>
      </View>
    ) : (
      <Image
        style={[styles.avatar, { height: diameter, width: diameter }]}
        source={{ uri: avatarUri }}
        accessibilityLabel={name === '' ? 'profile picture' : `${name}'s profile picture`}
      />
    );

  const nameText =
    name !== '' ? (
      <Text
        style={[
          styles.name,
          {
            fontSize: NAME_SIZE[size],
            fontWeight: size === 'small' ? fontWeights.medium : fontWeights.semiBold,
          },
        ]}
        numberOfLines={1}
      >
        {name}
      </Text>
    ) : null;

  return (
    <View style={styles.row}>
      {onPress !== undefined ? (
        <Pressable
          style={styles.identity}
          onPress={onPress}
          accessibilityRole="button"
          accessibilityLabel={name === '' ? 'Open profile' : `Open ${name}'s profile`}
        >
          {avatar}
          {nameText}
        </Pressable>
      ) : (
        <>
          {avatar}
          {nameText}
        </>
      )}

      {hasRooted ? (
        <RootedBadge
          place={rooted.place}
          durationBucket={rooted.duration_bucket}
          compact={size === 'small'}
        />
      ) : null}

      {relative !== '' ? (
        <Text style={[styles.time, { fontSize: TIME_SIZE[size] }]}>
          {hasLeadingSegment ? `· ${relative}` : relative}
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  avatar: {
    backgroundColor: colors.bg.secondary,
    borderRadius: radius.pill,
  },
  avatarFallback: {
    alignItems: 'center',
    backgroundColor: colors.brand.purple,
    borderRadius: radius.pill,
    justifyContent: 'center',
  },
  identity: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: spacing.sm,
  },
  initials: {
    color: colors.text.primary,
    fontWeight: fontWeights.semiBold,
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
  time: {
    color: colors.text.secondary,
  },
});

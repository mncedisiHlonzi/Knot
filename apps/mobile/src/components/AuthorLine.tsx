import React from 'react';
import { Image, StyleSheet, Text, View } from 'react-native';

import type { AuthorRooted } from '../api/rooted';
import { API_BASE_URL } from '../config/api';
import { colors, fontSizes, fontWeights, radius, spacing } from '../theme';
import { getInitials } from '../utils/initials';
import { formatRelativeTime } from '../utils/time';
import RootedBadge from './RootedBadge';

/** The three sizes AuthorLine offers. */
export type AuthorLineSize = 'small' | 'medium' | 'large';

type AuthorLineProps = {
  /** The author's display name. A blank name omits the name segment. */
  readonly displayName: string;
  /** The backend avatar path (`/users/{id}/avatar`), or null/empty for none. */
  readonly avatarUrl?: string | null;
  /** The content's ISO-8601 timestamp, rendered relative to now. */
  readonly createdAt?: string | null;
  /** The author's inline Rooted summary, or null when they have none. */
  readonly rooted?: AuthorRooted | null;
  /** Controls the avatar diameter and text weight. Defaults to "small". */
  readonly size?: AuthorLineSize;
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
 */
export default function AuthorLine({
  displayName,
  avatarUrl,
  createdAt,
  rooted,
  size = 'small',
}: AuthorLineProps): React.ReactElement {
  const name = displayName.trim();
  const diameter = AVATAR_DIAMETER[size];

  const path = avatarUrl === null || avatarUrl === undefined ? '' : avatarUrl.trim();
  const avatarUri = path === '' ? undefined : `${API_BASE_URL}${path}`;

  const relative =
    createdAt === null || createdAt === undefined || createdAt === ''
      ? ''
      : formatRelativeTime(createdAt);

  const hasRooted = rooted !== null && rooted !== undefined && rooted.place.trim() !== '';
  const hasLeadingSegment = name !== '' || hasRooted;

  return (
    <View style={styles.row}>
      {avatarUri === undefined ? (
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
      )}

      {name !== '' ? (
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
      ) : null}

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

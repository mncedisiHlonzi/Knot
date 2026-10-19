import React from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { API_BASE_URL } from '../config/api';
import { colors, spacing, typeScale } from '../theme';
import { formatRelativeTime, formatRelativeTimeShort } from '../utils/time';
import Avatar from './Avatar';
import RootedBadge from './RootedBadge';

/** The three sizes AuthorLine offers. */
export type AuthorLineSize = 'small' | 'medium' | 'large';

/**
 * How the timestamp reads. `long` is "3 days ago" and is the default every screen
 * already uses; `short` is the feed's compact "3d" (KNOT-023a).
 */
export type AuthorLineTimeFormat = 'long' | 'short';

/**
 * How the author's details are arranged. `inline` is the byline every screen uses:
 * name and time on one line. `stacked` is the Feed's social-card column: name,
 * then Rooted, then time, with the avatar to the left (KNOT-023b).
 */
export type AuthorLineLayout = 'inline' | 'stacked';

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
  /**
   * Controls the avatar diameter and the name's weight. Defaults to `small`
   * (24pt). `large` pairs the 48pt avatar with `typeScale.h3`, and is the Feed's
   * size (KNOT-020c-fix5).
   */
  readonly size?: AuthorLineSize;
  /**
   * Move the Rooted summary onto its own quieter second line, under the name and
   * time, instead of sitting inline beside them (KNOT-020c-fix2).
   *
   * Defaults to `false`, which keeps the inline attribution the existing screens
   * render. The Feed uses `layout="stacked"` rather than this prop; it remains for
   * a caller that wants the two-line inline form.
   */
  readonly showRootedOnNewLine?: boolean;
  /**
   * How to word the timestamp. Defaults to `long` ("3 days ago"), which is what
   * every existing screen gets; the Feed opts into `short` ("3d") because its
   * author block is already dense (KNOT-023a).
   */
  readonly timeFormat?: AuthorLineTimeFormat;
  /**
   * How the author's details are arranged. Defaults to `inline` (name and time on
   * one line), which is what every existing screen already draws. `stacked` makes
   * a left avatar and a three-line column — name, Rooted, time — for the Feed's
   * social-card look (KNOT-023b).
   */
  readonly layout?: AuthorLineLayout; /**
   * An optional marker drawn beside the name, such as the Feed's adaptation
   * glyph. It is a status marker, not a tap target, so it stays outside the
   * profile Pressable (KNOT-029).
   */
  readonly badge?: React.ReactNode; /**
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
 * and a comment all read the same way. Every segment is optional: a missing name or
 * timestamp is omitted rather than shown empty, and an author with no avatar gets
 * their initials in a circle instead (drawn by `Avatar`).
 *
 * Every field is validated as a string before it is used. The API response types
 * describe an optimistic wire format, so a response that omits an author field
 * delivers `undefined` at runtime; reading it must not crash the screen
 * (KNOT-015b-fix). A line with nothing at all to show renders `null` rather than an
 * empty row.
 *
 * When `showRootedOnNewLine` is set, the line becomes a block: the avatar stands
 * to the left and the text column — the name with its time, then the quiet Rooted
 * line — hangs off it, so the avatar's left edge is the visual guide for both
 * lines. (The Feed now uses `layout="stacked"` instead; the prop remains for
 * callers that want the two-line inline form.)
 *
 * `layout="stacked"` is the Feed's social-card block (KNOT-023b): the avatar on
 * the left, then a column of the name, the quiet Rooted line, and the time — each
 * on its own line, with no separators.
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
  timeFormat = 'long',
  layout = 'inline',
  badge,
  onPress,
  testID,
}: AuthorLineProps): React.ReactElement | null {
  const name = typeof displayName === 'string' ? displayName.trim() : '';

  const path = typeof avatarUrl === 'string' ? avatarUrl.trim() : '';
  const avatarUri = path === '' ? undefined : `${API_BASE_URL}${path}`;

  const relative =
    typeof createdAt === 'string' && createdAt !== ''
      ? timeFormat === 'short'
        ? formatRelativeTimeShort(createdAt)
        : formatRelativeTime(createdAt)
      : '';

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

  // The Feed's two-line form (KNOT-020c-fix2, refined in KNOT-020c-fix5): the
  // avatar sits to the left and the author's text column hangs off it, so the
  // name and the quiet Rooted line share the avatar's right edge as their left
  // guide.
  const rootedOnNewLine = showRootedOnNewLine === true && hasRooted;

  // The separator before the timestamp belongs only when something precedes it on
  // its line, and the Rooted badge no longer does when it has moved down.
  const hasLeadingSegment = name !== '' || (!rootedOnNewLine && hasRooted);

  const openProfileLabel = name === '' ? 'Open profile' : `Open ${name}'s profile`;

  const avatarNode = (
    <Avatar
      uri={avatarUri}
      displayName={typeof displayName === 'string' ? displayName : ''}
      size={AVATAR_SIZE[size]}
      accessibilityLabel={name === '' ? 'Profile picture' : `${name}'s profile picture`}
    />
  );

  const nameNode =
    name === '' ? null : (
      <Text
        style={[size === 'large' ? typeScale.h3 : typeScale.label, styles.name]}
        numberOfLines={1}
      >
        {name}
      </Text>
    );

  const timeText =
    relative === '' ? null : (
      <Text style={[typeScale.caption, styles.time]}>
        {hasLeadingSegment ? `· ${relative}` : relative}
      </Text>
    );

  // The Feed's stacked author block (KNOT-023b): the avatar on the left, and a
  // column on the right holding the name, the quiet Rooted line, then the time.
  // Each detail gets its own line, which is what makes the card read like a
  // social post rather than a byline. The name uses `typeScale.label` here even
  // when the caller asked for a larger avatar, because the column is a compact
  // identity block, not a headline.
  if (layout === 'stacked') {
    return (
      <View style={styles.stackedRow} testID={testID}>
        {onPress === undefined ? (
          avatarNode
        ) : (
          <Pressable
            onPress={onPress}
            accessibilityRole="button"
            accessibilityLabel={openProfileLabel}
          >
            {avatarNode}
          </Pressable>
        )}

        <View style={styles.stackedDetails}>
          <View style={styles.stackedNameRow}>
            {name === '' ? null : onPress === undefined ? (
              <Text style={[typeScale.label, styles.name]} numberOfLines={1}>
                {name}
              </Text>
            ) : (
              <Pressable
                style={styles.nameTarget}
                onPress={onPress}
                accessibilityRole="button"
                accessibilityLabel={openProfileLabel}
              >
                <Text style={[typeScale.label, styles.name]} numberOfLines={1}>
                  {name}
                </Text>
              </Pressable>
            )}
            {badge === undefined ? null : badge}
          </View>

          {hasRooted ? (
            <RootedBadge
              place={rootedPlace}
              durationBucket={durationBucket}
              variant="quiet"
              showDuration={false}
            />
          ) : null}

          {relative === '' ? null : (
            <Text style={[typeScale.caption, styles.time]}>{relative}</Text>
          )}
        </View>
      </View>
    );
  }

  // The Feed's author block: the avatar, then the text column — the name with its
  // time, and the quiet Rooted line beneath it, indented to the name's left edge
  // rather than spanning below the avatar. The Feed drops the duration, so its
  // line reads "Rooted · {place}" (KNOT-020c-fix5).
  if (rootedOnNewLine) {
    return (
      <View style={styles.feedRow} testID={testID}>
        {onPress === undefined ? (
          avatarNode
        ) : (
          <Pressable
            onPress={onPress}
            accessibilityRole="button"
            accessibilityLabel={openProfileLabel}
          >
            {avatarNode}
          </Pressable>
        )}

        <View style={styles.details}>
          <View style={styles.nameRow}>
            {nameNode === null ? null : onPress === undefined ? (
              nameNode
            ) : (
              <Pressable
                style={styles.nameTarget}
                onPress={onPress}
                accessibilityRole="button"
                accessibilityLabel={openProfileLabel}
              >
                {nameNode}
              </Pressable>
            )}
            {badge === undefined ? null : badge}
            {timeText}
          </View>

          <RootedBadge
            place={rootedPlace}
            durationBucket={durationBucket}
            variant="quiet"
            showDuration={false}
          />
        </View>
      </View>
    );
  }

  const identity = (
    <>
      {avatarNode}
      {nameNode}
    </>
  );

  const identityTarget =
    onPress !== undefined ? (
      <Pressable
        style={styles.identity}
        onPress={onPress}
        accessibilityRole="button"
        accessibilityLabel={openProfileLabel}
      >
        {identity}
      </Pressable>
    ) : (
      <View style={styles.identity}>{identity}</View>
    );

  return (
    <View style={styles.row} testID={testID}>
      {identityTarget}
      {badge === undefined ? null : badge}

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
  // The Feed's author block: avatar beside the text column, both centred against
  // each other so the avatar's left edge guides the name and the Rooted line.
  details: {
    flexShrink: 1,
    gap: spacing.xs,
  },
  feedRow: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: spacing.sm,
  },
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
  // The name and time line inside the Feed's text column.
  nameRow: {
    alignItems: 'center',
    flexDirection: 'row',
    flexShrink: 1,
    gap: spacing.sm,
  },
  // Lets a long name truncate inside its own tap target.
  nameTarget: {
    flexShrink: 1,
  },
  // The Feed's stacked block: a left avatar and a column of name, Rooted, time.
  // The column has no gap: the three tokens' own line heights (16/16/16) already
  // give the block its rhythm, and a gap made the identity read as three
  // separated facts rather than one unit (KNOT-023c).
  stackedDetails: {
    flexShrink: 1,
  },
  // The name and an optional status marker share one line, so the marker sits
  // beside the name rather than under it (KNOT-029).
  stackedNameRow: {
    alignItems: 'center',
    flexDirection: 'row',
    flexShrink: 1,
    gap: spacing.xs,
  },
  stackedRow: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: spacing.sm,
  },
  row: {
    alignItems: 'center',
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
  },
  time: {
    color: colors.text.tertiary,
  },
});

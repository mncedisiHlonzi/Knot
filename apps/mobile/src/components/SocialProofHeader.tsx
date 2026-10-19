import React from 'react';
import { StyleSheet, Text, View } from 'react-native';

import type { RecentInteraction } from '../api/stories';
import { API_BASE_URL } from '../config/api';
import { colors, radius, spacing, typeScale } from '../theme';
import Avatar from './Avatar';
import { attributionRuns, socialProofActors, socialProofRuns } from './socialProof';

/** One face in the header's left-hand stack. */
export type SocialProofAvatar = {
  readonly key: string;
  readonly uri: string | undefined;
  readonly name: string;
  readonly label: string;
};

type SocialProofHeaderProps = {
  /**
   * The entry's latest interactions, newest first. At most two are drawn; the
   * server has already merged one reaction and one comment (KNOT-028).
   */
  readonly interactions?: readonly RecentInteraction[];
  /** The server-rendered hereditary sentence, used when there are no interactions. */
  readonly attributionLine?: string;
  /** The original author's name, for the structured fallback sentence. */
  readonly originalAuthorName?: string;
  /** The lineage faces the attribution fallback draws, in credit order. */
  readonly lineageAvatars?: readonly SocialProofAvatar[];
};

/** The header's line limit: it is a caption above the card, not a paragraph. */
const SOCIAL_PROOF_LINES = 2;

/**
 * Resolves an avatar path against the API base. A blank path becomes `undefined`,
 * which `Avatar` reads as "draw initials".
 */
function resolveAvatarUrl(path: string | null | undefined): string | undefined {
  const trimmed = typeof path === 'string' ? path.trim() : '';
  return trimmed === '' ? undefined : `${API_BASE_URL}${trimmed}`;
}

/** The accessibility label for an actor with no resolvable name. */
function avatarLabel(name: string): string {
  return name === '' ? 'Profile picture' : `${name}'s profile picture`;
}

/**
 * The social-proof header at the top of a feed card (KNOT-028, universal since
 * KNOT-029).
 *
 * It draws one of three states, in this order:
 *
 * 1. **Interactions exist** — one avatar per actor beside a natural sentence
 *    naming what each did ("Tycoon finds this true", "Tycoon finds this true and
 *    commented", "Tycoon & Yantuntu know this differently", "Tycoon finds this
 *    true and Yantuntu commented on this"). A person who took two actions is
 *    drawn once and named once (KNOT-030).
 * 2. **No interactions, but attribution exists** — the lineage faces beside the
 *    server's hereditary sentence, with every name bolded. This is the card's
 *    pre-KNOT-028 header, unchanged. Only a version card has this input; a story
 *    card draws state 1 or 3.
 * 3. **Neither** — nothing at all.
 *
 * The decision and the wording live in `socialProof.ts`; this component only
 * maps runs onto the design system's two inks. A hairline closes the header so
 * the credit reads as distinct from the poster's author block below it.
 *
 * The header is a caption, not a paragraph: it is a step down in type from the
 * author block it introduces, its faces are the smallest avatar, and it hugs the
 * card's edges rather than opening a gap above them (KNOT-031).
 */
export default function SocialProofHeader({
  interactions,
  attributionLine,
  originalAuthorName = '',
  lineageAvatars = [],
}: SocialProofHeaderProps): React.ReactElement | null {
  const actors = socialProofActors(interactions);
  const runs =
    actors.length > 0
      ? socialProofRuns(interactions)
      : attributionRuns(attributionLine, originalAuthorName);

  if (runs.length === 0) {
    return null;
  }

  // With interactions, the stack is one face per ACTOR (newest first), so a
  // person who both reacted and commented is drawn once (KNOT-030). With none,
  // it is the lineage the attribution credits.
  const avatars: readonly SocialProofAvatar[] =
    actors.length > 0
      ? actors.map((actor, index) => ({
          key: `actor-${index}`,
          uri: resolveAvatarUrl(actor.avatarUrl),
          name: actor.name,
          label: avatarLabel(actor.name),
        }))
      : lineageAvatars;

  return (
    <View style={styles.container} testID="social-proof-header">
      <View style={styles.row}>
        {avatars.length > 0 ? (
          <View style={styles.avatars}>
            {avatars.map((actor, index) => (
              <View key={actor.key} style={index === 0 ? null : styles.avatarStacked}>
                <Avatar
                  uri={actor.uri}
                  displayName={actor.name}
                  size="xs"
                  accessibilityLabel={actor.label}
                />
              </View>
            ))}
          </View>
        ) : null}

        <Text
          testID="social-proof-sentence"
          style={[typeScale.caption, styles.text]}
          numberOfLines={SOCIAL_PROOF_LINES}
        >
          {runs.map((run, index) =>
            run.bold ? (
              <Text key={index} style={[typeScale.label, styles.name]}>
                {run.text}
              </Text>
            ) : (
              run.text
            ),
          )}
        </Text>
      </View>

      {/* The rule between the header and the poster's author block (KNOT-023c). */}
      <View style={styles.divider} testID="attribution-divider" />
    </View>
  );
}

const styles = StyleSheet.create({
  // Each later face sits over the one before it, ringed in the canvas so the
  // faces stay legible where they meet (KNOT-022-fix2). The overlap is one
  // spacing step, which is what an xs face can carry without hiding the one
  // beneath (KNOT-031).
  avatarStacked: {
    backgroundColor: colors.bg.canvas,
    borderColor: colors.bg.canvas,
    borderRadius: radius.pill,
    borderWidth: 2,
    marginLeft: -spacing.xs,
  },
  avatars: {
    alignItems: 'center',
    flexDirection: 'row',
  },
  // The header sits flush at the top of the card: a single small vertical step,
  // no opening gap. The rule below it stays the design system's hairline — it
  // separates two blocks inside one card, unlike the heavier rule BETWEEN cards
  // (KNOT-031).
  container: {
    paddingVertical: spacing.xs,
  },
  // The rule between the credit and the poster's author block (KNOT-023c). It is
  // the design system's hairline, not the heavier divider, and it hugs the author
  // block below it (KNOT-031).
  divider: {
    backgroundColor: colors.border.hairline,
    height: 1,
    marginBottom: spacing.xs,
    marginTop: 0,
  },
  name: {
    color: colors.text.primary,
  },
  // Stacked avatars on the left, the credit on the right: the LinkedIn header's
  // weight (KNOT-028).
  row: {
    alignItems: 'center',
    flexDirection: 'row',
    gap: spacing.sm,
  },
  text: {
    color: colors.text.secondary,
    flexShrink: 1,
  },
});

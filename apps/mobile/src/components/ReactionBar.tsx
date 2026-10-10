import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Pressable, StyleSheet, Text, View } from 'react-native';

import { describeError } from '../api/client';
import {
  ReactionCounts,
  ReactionEntityType,
  ReactionType,
  REACTION_EMOJI,
  REACTION_LABELS,
  REACTION_TYPES,
  reactionsApi,
} from '../api/reactions';
import { colors, fontSizes, fontWeights, radius, spacing } from '../theme';

type ReactionBarProps = {
  /** The kind of content being reacted to. */
  readonly entityType: ReactionEntityType;
  /** The id of that content. */
  readonly entityId: string;
  /** The counts the server last reported, or the empty summary. */
  readonly counts: ReactionCounts;
  /** The signals the signed-in reader holds, as reaction-type strings. */
  readonly myReactions: readonly string[];
  /** The reader's access token, or undefined when signed out. */
  readonly token?: string;
  /**
   * Called after an optimistic change with the new counts and the reader's own
   * signals, so the parent keeps its copy in step across a re-render.
   */
  readonly onChange: (counts: ReactionCounts, myReactions: readonly ReactionType[]) => void;
  /** When true, only the emoji and count are drawn (for dense list rows). */
  readonly compact?: boolean;
};

/** Reports whether the reader holds one signal. */
function holds(mine: readonly string[], type: ReactionType): boolean {
  return mine.includes(type);
}

/**
 * The four perspective signals on one piece of content, as a row of tappable
 * chips.
 *
 * A chip reads `[emoji] [label] [count]` — the count is hidden while it is zero,
 * and the label is hidden in the compact variant. A signal the reader holds is
 * highlighted. Tapping toggles it: the change is applied optimistically and
 * reverted if the server refuses.
 *
 * The bar is a perspective, not a like: the four chips never compete, and a
 * reader may hold any combination (KNOT-ADR-050). Signed-out readers are invited
 * to sign in rather than shown a control that cannot work.
 */
export default function ReactionBar({
  entityType,
  entityId,
  counts,
  myReactions,
  token,
  onChange,
  compact = false,
}: ReactionBarProps): React.ReactElement {
  const [localCounts, setLocalCounts] = useState<ReactionCounts>(counts);
  const [localMine, setLocalMine] = useState<readonly string[]>(myReactions);
  const [busy, setBusy] = useState(false);

  // Re-seed from the server whenever the parent's copy changes, so a refresh or a
  // navigation back into the screen shows the authoritative counts rather than a
  // stale optimistic value.
  useEffect(() => {
    setLocalCounts(counts);
    setLocalMine(myReactions);
  }, [counts, myReactions]);

  const toggle = useCallback(
    async (type: ReactionType): Promise<void> => {
      if (token === undefined || token === '') {
        Alert.alert('Sign in to react', 'Reactions are saved to your account.');
        return;
      }
      if (busy) {
        return;
      }

      const had = holds(localMine, type);
      const nextMine: ReactionType[] = had
        ? (localMine.filter((tag) => tag !== type) as ReactionType[])
        : [...(localMine as ReactionType[]), type];
      const nextCounts: ReactionCounts = {
        ...localCounts,
        [type]: Math.max(0, localCounts[type] + (had ? -1 : 1)),
      };

      // Optimistic: show the change at once.
      setLocalCounts(nextCounts);
      setLocalMine(nextMine);
      onChange(nextCounts, nextMine);
      setBusy(true);

      try {
        const result = await reactionsApi.toggleReaction(entityType, entityId, type, token);
        setLocalCounts(result.reactions);
        onChange(result.reactions, nextMine);
      } catch (caught) {
        // Revert to what we had before the tap.
        setLocalCounts(localCounts);
        setLocalMine(localMine);
        onChange(localCounts, localMine as ReactionType[]);
        Alert.alert('Could not react', describeError(caught));
      } finally {
        setBusy(false);
      }
    },
    [busy, entityId, entityType, localCounts, localMine, onChange, token],
  );

  return (
    <View style={styles.bar}>
      {REACTION_TYPES.map((type) => {
        const active = holds(localMine, type);
        const count = localCounts[type];

        return (
          <Pressable
            key={type}
            style={[
              styles.chip,
              compact ? styles.chipCompact : null,
              active ? styles.chipActive : null,
            ]}
            onPress={() => {
              void toggle(type);
            }}
            accessibilityRole="button"
            accessibilityState={{ selected: active }}
            accessibilityLabel={`${REACTION_LABELS[type]}${count > 0 ? `, ${count}` : ''}`}
          >
            <Text style={styles.emoji}>{REACTION_EMOJI[type]}</Text>
            {compact ? null : (
              <Text style={[styles.label, active ? styles.activeText : null]}>
                {REACTION_LABELS[type]}
              </Text>
            )}
            {count > 0 ? (
              <Text style={[styles.count, active ? styles.activeText : null]}>{count}</Text>
            ) : null}
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  activeText: {
    color: colors.text.primary,
  },
  bar: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
    marginTop: spacing.md,
  },
  chip: {
    alignItems: 'center',
    backgroundColor: colors.bg.primary,
    borderColor: colors.border.default,
    borderRadius: radius.pill,
    borderWidth: 1,
    flexDirection: 'row',
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
  },
  chipActive: {
    backgroundColor: colors.brand.purple,
    borderColor: colors.brand.purple,
  },
  chipCompact: {
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
  },
  count: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    fontWeight: fontWeights.semiBold,
    marginLeft: spacing.xs,
  },
  emoji: {
    fontSize: fontSizes.sm,
  },
  label: {
    color: colors.text.secondary,
    fontSize: fontSizes.sm,
    marginLeft: spacing.xs,
  },
});

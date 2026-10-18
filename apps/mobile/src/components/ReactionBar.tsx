import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Pressable, StyleSheet, Text, View } from 'react-native';

import { describeError } from '../api/client';
import {
  ReactionCounts,
  ReactionType,
  REACTION_LABELS,
  REACTION_TYPES,
  reactionsApi,
} from '../api/reactions';
import { colors, radius, spacing, tabularNums, typeScale } from '../theme';
import { applyReactionToggle, asReactionTypes, holdsReaction } from './reactionToggle';

/** The content kinds a reaction can target that the app exposes today. */
export type ReactionBarEntityType = 'story' | 'version' | 'inquiry';

type ReactionBarProps = {
  /** The kind of content being reacted to. */
  readonly entityType: ReactionBarEntityType;
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
  /**
   * The bar's geometry. `row` (the default) is the four equal columns every list
   * and detail currently uses; `grid` is the Story Detail's two-by-two block,
   * where each signal has twice the width and its label fits on one line
   * (KNOT-025-fix2).
   */
  readonly layout?: 'row' | 'grid';
};

/**
 * The four perspective signals on one piece of content, as a row of equal text
 * pills.
 *
 * Each pill reads `[label] [count]`; the count is hidden while it is zero. A
 * signal the reader holds takes the brand tint fill and brand ink, so a selection
 * is a visible surface change rather than a tinted glyph. The custom reaction
 * SVGs are deliberately gone (KNOT-025-fix3): the pills are more legible at small
 * sizes, match the modern social pattern, and remove the one-off icon set from
 * the app.
 *
 * `layout="row"` is the four-column bar every existing surface uses; the Story
 * Detail opts into `layout="grid"`, which gives each pill half the width so its
 * label never wraps. Tapping toggles a signal optimistically and reverts it if
 * the server refuses.
 *
 * The bar is a perspective, not a like: the four signals never compete, and a
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
  layout = 'row',
}: ReactionBarProps): React.ReactElement {
  const inGrid = layout === 'grid';
  const [localCounts, setLocalCounts] = useState<ReactionCounts>(counts);
  const [localMine, setLocalMine] = useState<readonly ReactionType[]>(() =>
    asReactionTypes(myReactions),
  );
  const [busy, setBusy] = useState(false);

  // Re-seed from the server whenever the parent's copy changes, so a refresh or a
  // navigation back into the screen shows the authoritative counts rather than a
  // stale optimistic value.
  useEffect(() => {
    setLocalCounts(counts);
    setLocalMine(asReactionTypes(myReactions));
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

      // Optimistic: show the change at once.
      const optimistic = applyReactionToggle(localMine, localCounts, type);
      setLocalCounts(optimistic.counts);
      setLocalMine(optimistic.myReactions);
      onChange(optimistic.counts, optimistic.myReactions);
      setBusy(true);

      try {
        const result = await reactionsApi.toggleReaction(entityType, entityId, type, token);
        setLocalCounts(result.reactions);
        onChange(result.reactions, optimistic.myReactions);
      } catch (caught) {
        // Revert to what we had before the tap.
        setLocalCounts(localCounts);
        setLocalMine(localMine);
        onChange(localCounts, localMine);
        Alert.alert('Could not react', describeError(caught));
      } finally {
        setBusy(false);
      }
    },
    [busy, entityId, entityType, localCounts, localMine, onChange, token],
  );

  return (
    <View style={[styles.bar, inGrid ? styles.grid : null]}>
      {REACTION_TYPES.map((type) => {
        const active = holdsReaction(localMine, type);
        const count = localCounts[type];

        return (
          <Pressable
            key={type}
            hitSlop={spacing.xs}
            style={[
              styles.pill,
              inGrid ? styles.gridPill : styles.rowPill,
              active ? styles.pillActive : styles.pillIdle,
            ]}
            onPress={() => {
              void toggle(type);
            }}
            accessibilityRole="button"
            accessibilityState={{ selected: active }}
            accessibilityLabel={`${REACTION_LABELS[type]}${count > 0 ? `, ${count}` : ''}`}
          >
            <Text
              style={[
                typeScale.label,
                inGrid ? styles.gridLabel : styles.rowLabel,
                active ? styles.activeText : styles.idleLabel,
              ]}
              numberOfLines={inGrid ? 1 : 2}
            >
              {REACTION_LABELS[type]}
            </Text>
            {count > 0 ? (
              <Text
                style={[
                  typeScale.label,
                  tabularNums,
                  styles.count,
                  active ? styles.activeText : null,
                ]}
              >
                {count}
              </Text>
            ) : null}
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  activeText: {
    color: colors.brand.primary,
  },
  bar: {
    flexDirection: 'row',
    marginTop: spacing.md,
  },
  count: {
    color: colors.text.tertiary,
  },
  // Two columns, each just under half the width so the space-between gutter keeps
  // adjacent pills apart. The label gets the whole pill and stays on one line.
  grid: {
    flexWrap: 'wrap',
    justifyContent: 'space-between',
    rowGap: spacing.xs,
  },
  gridLabel: {
    flexShrink: 1,
    textAlign: 'left',
  },
  gridPill: {
    height: 36,
    minHeight: 36,
    paddingHorizontal: spacing.sm,
    width: '49%',
  },
  idleLabel: {
    color: colors.text.secondary,
  },
  // The shared pill: a bordered paper surface at rest, the brand tint when held.
  // The grid holds it to 36pt for a consistent rhythm; `hitSlop` keeps the touch
  // target at the design system's 44pt minimum.
  pill: {
    alignItems: 'center',
    borderColor: colors.border.hairline,
    borderRadius: radius.pill,
    borderWidth: 1,
    flexDirection: 'row',
    gap: spacing.xs,
    justifyContent: 'space-between',
    minHeight: 44,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  pillActive: {
    backgroundColor: colors.brand.tint,
  },
  pillIdle: {
    backgroundColor: colors.bg.paper,
  },
  // The four-column bar the Feed, version, and inquiry surfaces use. The count
  // sits under the label so a narrow column still reads as one unit.
  rowLabel: {
    textAlign: 'center',
  },
  rowPill: {
    flex: 1,
    flexDirection: 'column',
    gap: spacing.xs,
    justifyContent: 'center',
    marginHorizontal: spacing.xs,
    paddingHorizontal: spacing.xs,
  },
});

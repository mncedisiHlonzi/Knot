import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Pressable, StyleSheet, Text, View } from 'react-native';
import type { SvgProps } from 'react-native-svg';

import AddSomethingNewGlyph from '../assets/brand/reaction-adds-something-new.svg';
import KnowItDifferentlyGlyph from '../assets/brand/reaction-know-it-differently.svg';
import NeedsASourceGlyph from '../assets/brand/reaction-needs-a-source.svg';
import RingsTrueGlyph from '../assets/brand/reaction-rings-true.svg';
import { describeError } from '../api/client';
import {
  ReactionCounts,
  ReactionType,
  REACTION_LABELS,
  REACTION_TYPES,
  reactionsApi,
} from '../api/reactions';
import { colors, fontWeights, radius, spacing, tabularNums, typeScale } from '../theme';
import { applyReactionToggle, asReactionTypes, holdsReaction } from './reactionToggle';
import SvgImage from './SvgImage';

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
  /** When true, only the icon and count are drawn (for dense list rows). */
  readonly compact?: boolean;
};

/**
 * The brand's own reaction glyph for each signal (KNOT-020b-fix). Each asset is a
 * 24 x 24 stroke drawn with `currentColor`, so it is tinted through the `color`
 * prop of `SvgImage`. The fifth glyph, `reaction-bridge.svg`, is an unused brand
 * asset: reactions target stories, versions, comments, and inquiries only.
 */
const REACTION_ICONS: Readonly<Record<ReactionType, React.ComponentType<SvgProps>>> = {
  rings_true: RingsTrueGlyph,
  know_it_differently: KnowItDifferentlyGlyph,
  adds_something_new: AddSomethingNewGlyph,
  needs_a_source: NeedsASourceGlyph,
};

/** The icon size drawn in each column. */
const ICON_SIZE = 22;

/**
 * The four perspective signals on one piece of content, as a row of equal columns.
 *
 * Each column reads `[glyph] [label] [count]` — the count is hidden while it is
 * zero, and the label is hidden in the compact variant. A signal the reader holds
 * is highlighted on three counts: the column takes the brand tint fill, the glyph
 * and text take the brand colour, and the label takes the semibold weight — so a
 * selection is paired with weight rather than signalled by colour alone. Tapping
 * toggles it: the change is applied optimistically and reverted if the server
 * refuses.
 *
 * The bar is a perspective, not a like: the four columns never compete, and a
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
    <View style={styles.bar}>
      {REACTION_TYPES.map((type) => {
        const active = holdsReaction(localMine, type);
        const count = localCounts[type];
        const IconComponent = REACTION_ICONS[type];
        const tint = active ? colors.brand.primary : colors.text.tertiary;

        return (
          <Pressable
            key={type}
            style={[styles.column, active ? styles.columnActive : null]}
            onPress={() => {
              void toggle(type);
            }}
            accessibilityRole="button"
            accessibilityState={{ selected: active }}
            accessibilityLabel={`${REACTION_LABELS[type]}${count > 0 ? `, ${count}` : ''}`}
          >
            <SvgImage source={IconComponent} size={ICON_SIZE} color={tint} decorative />
            {compact ? null : (
              <Text
                style={[typeScale.caption, styles.label, active ? styles.activeText : null]}
                numberOfLines={2}
              >
                {REACTION_LABELS[type]}
              </Text>
            )}
            {count > 0 ? (
              <Text
                style={[
                  typeScale.caption,
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
    fontWeight: fontWeights.semibold,
  },
  bar: {
    flexDirection: 'row',
    marginTop: spacing.md,
  },
  column: {
    alignItems: 'center',
    borderRadius: radius.lg,
    flex: 1,
    gap: spacing.xs,
    justifyContent: 'flex-start',
    minHeight: 44,
    paddingHorizontal: spacing.xs,
    paddingVertical: spacing.sm,
  },
  columnActive: {
    backgroundColor: colors.brand.tint,
  },
  count: {
    color: colors.text.tertiary,
  },
  label: {
    color: colors.text.tertiary,
    textAlign: 'center',
  },
});

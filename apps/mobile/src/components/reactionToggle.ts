import { ReactionCounts, ReactionType, REACTION_TYPES } from '../api/reactions';

/** The set of valid reaction types, for cheap membership checks. */
const REACTION_TYPE_SET: ReadonlySet<string> = new Set(REACTION_TYPES);

/**
 * Narrows a list of signal strings to the closed `ReactionType` union.
 *
 * The API types a reader's own signals as plain strings (a response can carry a
 * value this build does not know), so anything unrecognised is dropped rather than
 * forwarded. This keeps the cast-free narrowing in one place.
 */
export function asReactionTypes(tags: readonly string[]): ReactionType[] {
  return tags.filter((tag): tag is ReactionType => REACTION_TYPE_SET.has(tag));
}

/** Reports whether the reader holds one signal. */
export function holdsReaction(mine: readonly string[], type: ReactionType): boolean {
  return mine.includes(type);
}

/** The result of an optimistic toggle. */
export type ReactionToggleResult = {
  /** The counts with the toggled signal adjusted. */
  readonly counts: ReactionCounts;
  /** The reader's signals with the toggled signal added or removed. */
  readonly myReactions: ReactionType[];
};

/**
 * Computes the optimistic result of toggling one signal.
 *
 * A signal the reader already holds is removed (count - 1, floored at zero);
 * otherwise it is added (count + 1). The four signals never interact, so only the
 * toggled one changes.
 */
export function applyReactionToggle(
  mine: readonly string[],
  counts: ReactionCounts,
  type: ReactionType,
): ReactionToggleResult {
  const had = holdsReaction(mine, type);
  const current = asReactionTypes(mine);

  return {
    counts: { ...counts, [type]: Math.max(0, counts[type] + (had ? -1 : 1)) },
    myReactions: had ? current.filter((tag) => tag !== type) : [...current, type],
  };
}

/**
 * The viewability rule behind every screen that plays a story's video in place.
 *
 * A list that can hold more than one video has to decide which single one is "on
 * screen" at any moment: two videos playing at once is noise and wasted decoding.
 * The rule is the same on every screen — an item counts once it is at least 60%
 * visible, and the first such item becomes the active one — so it lives here once
 * rather than being re-declared per screen.
 *
 * The module touches nothing from React Native, so the decision can be tested
 * without a renderer, the same split `mediaFrame.ts` uses (KNOT-020g-fix7).
 */

/**
 * How much of an item must be on screen before it becomes the active one.
 *
 * At 60% only one item can normally qualify at a time, which is what keeps
 * playback to a single video per screen (KNOT-ADR-068).
 */
export const VIEWABILITY_CONFIG = { itemVisiblePercentThreshold: 60 };

/**
 * The rule a list that plays a clip **inside its own rows** uses (KNOT-050).
 *
 * It is measured as a share of the VIEWPORT (`viewAreaCoveragePercentThreshold`)
 * rather than as a share of the item, because a feed card is about as tall as the
 * viewport and often taller: "90% of the card is visible" is unreachable for any
 * card above about 1.11× the viewport, so that card's video would never play.
 *
 * The number is a **majority**, not "almost all". Two cards share a feed screen
 * nearly all the time — a card is taller than the viewport, so the next one is
 * always partly on screen — and a strict 90% would mean no card ever qualifies and
 * no video ever plays. At 50 the card that owns most of the screen is the one that
 * plays, the player switches exactly at the halfway point, and two cards can only
 * both qualify on an exact split (where the first, upper card wins). Everything
 * below the threshold is a still, which is the memory bound the inline placement
 * rests on.
 */
export const INLINE_VIEWABILITY_CONFIG = { viewAreaCoveragePercentThreshold: 50 };

/**
 * The slice of a viewability callback's payload a screen reads.
 *
 * It is structural on purpose: the callback receives React Native's `ViewToken`
 * objects, but a screen only ever needs each item's `key`, so widening the type
 * here keeps the caller clear of the library type and lets a test build the
 * payload from a plain object (the `useRef`-held callback must be typed).
 */
export type ViewableItemsInfo = {
  readonly viewableItems: ReadonlyArray<{
    readonly key: string | number;
    /** The item's position in the list, which the Feed tracks (KNOT-049). */
    readonly index?: number | null;
  }>;
};

/**
 * The key of the item a list should treat as active — the first item the list
 * reports as sufficiently visible, or `undefined` when none is.
 *
 * The list reports items in the order they appear, so the first is the uppermost
 * one on screen; with the 60% threshold there is normally only ever one. The key
 * is always a string, because it is compared against a list item's `id`.
 */
export function activeViewableKey(info: ViewableItemsInfo): string | undefined {
  const first = info.viewableItems[0];
  return first === undefined ? undefined : String(first.key);
}

/**
 * The INDEX of the item a list should treat as active, or `undefined` when none
 * is sufficiently visible.
 *
 * The Feed tracks a position rather than a key, because the position is what it
 * compares against each row as the list builds it — one number instead of a
 * lookup per card, and nothing to go stale when an entry is hidden (KNOT-049).
 * An item reported without a usable index answers `undefined`, which reads as
 * "no card is settled", never as "card zero" — a wrong zero would mount a player
 * on the first row.
 */
export function activeViewableIndex(info: ViewableItemsInfo): number | undefined {
  const index = info.viewableItems[0]?.index;
  return typeof index === 'number' && Number.isInteger(index) && index >= 0 ? index : undefined;
}

/**
 * Whether one row may hold a video player right now.
 *
 * The row must be the active one, and a row that is not active must never be
 * treated as active because the index is also missing (`undefined === undefined` is
 * true, which would mount a player on every row). The list's own scrolling no
 * longer matters: the player is part of the card, so it scrolls with it (KNOT-050).
 */
export function isActiveEntry(activeIndex: number | undefined, index: number): boolean {
  return activeIndex !== undefined && index === activeIndex;
}

/**
 * The key of the item AFTER the active one, so a list can prepare it before it is
 * on screen.
 *
 * The feed hands this to the next video's player, which stays mounted and paused so
 * the platform can buffer its first seconds; by the time it becomes the active item
 * the bytes are already there and playback starts immediately (KNOT-037-fix7).
 *
 * It answers `undefined` when nothing is active, when the active key is not in the
 * list any more (an entry was hidden), or when it is the last item — there is
 * nothing ahead of the end.
 */
export function nextKeyAfter(
  keys: readonly string[],
  activeKey: string | undefined,
): string | undefined {
  if (activeKey === undefined) {
    return undefined;
  }

  const index = keys.indexOf(activeKey);
  if (index < 0 || index + 1 >= keys.length) {
    return undefined;
  }

  return keys[index + 1];
}

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
 * The slice of a viewability callback's payload a screen reads.
 *
 * It is structural on purpose: the callback receives React Native's `ViewToken`
 * objects, but a screen only ever needs each item's `key`, so widening the type
 * here keeps the caller clear of the library type and lets a test build the
 * payload from a plain object (the `useRef`-held callback must be typed).
 */
export type ViewableItemsInfo = {
  readonly viewableItems: ReadonlyArray<{ readonly key: string | number }>;
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

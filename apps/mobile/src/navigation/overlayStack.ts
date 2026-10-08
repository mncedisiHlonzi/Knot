/**
 * A tiny, pure overlay-stack model.
 *
 * The app has two navigation levels: an active tab (Home, Map, Create, Profile)
 * and a stack of overlay screens pushed over it (a story, its tree, its
 * conversation, a place's stories, and so on). This module is only the array
 * arithmetic for that stack — no React, no state — so it is trivial to test and
 * the state itself can stay in `App.tsx` (KNOT-ADR-025).
 *
 * The helpers are generic over the element type rather than fixed to a concrete
 * screen shape. `App.tsx` keeps a discriminated union of overlays so the
 * compiler proves that each overlay carries exactly the ids its screen needs;
 * these helpers just move those values around without caring what they are.
 * {@link OverlayScreen} documents the loose `{ name, props }` shape an overlay
 * could take if a screen were ever pushed without a compile-time-known payload.
 */

/** A loosely-typed overlay: a screen name and the props to render it with. */
export type OverlayScreen = {
  readonly name: string;
  readonly props: Record<string, unknown>;
};

/** Adds an overlay to the top of the stack. Returns a new array. */
export function pushOverlay<T>(stack: readonly T[], overlay: T): readonly T[] {
  return [...stack, overlay];
}

/** Removes the top overlay. An empty stack is returned unchanged. */
export function popOverlay<T>(stack: readonly T[]): readonly T[] {
  return stack.length === 0 ? stack : stack.slice(0, -1);
}

/**
 * Replaces the top overlay without changing the stack's depth — used when one
 * screen hands off to its successor in place (adapting a story, then showing its
 * tree). On an empty stack it simply pushes.
 */
export function replaceOverlay<T>(stack: readonly T[], overlay: T): readonly T[] {
  return stack.length === 0 ? [overlay] : [...stack.slice(0, -1), overlay];
}

/**
 * Empties the stack — used on sign-out and whenever the app returns to a tab.
 * An already-empty stack is returned as-is.
 */
export function popAllOverlays<T>(stack: readonly T[]): readonly T[] {
  return stack.length === 0 ? stack : [];
}

/** The top overlay, or `undefined` when the stack is empty. */
export function topOverlay<T>(stack: readonly T[]): T | undefined {
  return stack.length === 0 ? undefined : stack[stack.length - 1];
}

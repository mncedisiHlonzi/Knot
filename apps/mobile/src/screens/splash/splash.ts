/**
 * Splash-screen logic (KNOT-020c).
 *
 * The splash is a short, branded cold-start screen with no controls: it holds
 * the canvas while the stored session and the first-run flag are read from
 * storage, then hands off to the next destination. The timing and the hand-off
 * decision are kept here as plain values and a pure function so they can be
 * tested without a renderer (the project has none).
 */

/** How long the splash stays on screen on a cold start, in milliseconds. */
export const SPLASH_DURATION_MS = 1200;

/**
 * Where a cold start goes once the splash has elapsed.
 *
 * `onboarding` is the first-run flow; `app` is everything after it — the feed
 * when there is a session, or the auth screens when there is not (the app root
 * decides which, because both are "past the boot flow").
 */
export type PostSplashDestination = 'app' | 'onboarding';

/**
 * The destination to show once the splash has elapsed.
 *
 * A returning signed-in user goes straight to the app. A signed-in user has
 * already been onboarded (onboarding ends at the auth screens), so a session
 * always wins. With no session, a first-run device sees onboarding and a device
 * that has seen it goes to the auth screens.
 */
export function postSplashDestination(
  hasSession: boolean,
  hasOnboarded: boolean,
): PostSplashDestination {
  if (hasSession) {
    return 'app';
  }
  return hasOnboarded ? 'app' : 'onboarding';
}

/**
 * Appearance persistence for the Knot mobile app (KNOT-020k).
 *
 * The appearance is a device preference, not an account setting: it lives in
 * AsyncStorage beside the session, under its own versioned key, so switching
 * device or signing out does not move it.
 *
 * There are three values — **system**, light and dark — because "follow the OS"
 * is a choice in its own right, not the absence of one:
 *
 *   system  follow the device's appearance, live (the default)
 *   light   always light
 *   dark    always dark
 *
 * `knot.theme.v1` (KNOT-020i) stored a two-value choice. It is read exactly once
 * when no appearance has been stored yet, so a user who picked dark before this
 * task keeps it instead of being dropped back to the system default. After the
 * first write the new key owns the preference and the old one is ignored.
 *
 * Storage is best-effort throughout, exactly as `session.ts` treats it: a failure
 * is logged and swallowed, and a missing or corrupted value means "no stored
 * preference", which lets the app follow the system. The app must never crash —
 * or refuse to start — because storage was unavailable.
 */
import AsyncStorage from '@react-native-async-storage/async-storage';

import { loadTheme } from './themeStorage';

/** The AsyncStorage key the appearance is stored under. */
export const APPEARANCE_KEY = 'knot.appearance.v1';

/**
 * The three ways the app can choose a mode.
 *
 * `system` is stored explicitly rather than by clearing the key, so "follow the
 * device" survives a restart and is distinguishable from "never chosen".
 */
export type AppearanceMode = 'system' | 'light' | 'dark';

/** Reports whether a parsed value is one of the modes we know. */
function isAppearanceMode(value: unknown): value is AppearanceMode {
  return value === 'system' || value === 'light' || value === 'dark';
}

/**
 * Persists the chosen appearance.
 *
 * A storage failure is logged and swallowed: the choice still applies for this
 * run, it simply will not survive a restart.
 */
export async function saveAppearance(mode: AppearanceMode): Promise<void> {
  try {
    await AsyncStorage.setItem(APPEARANCE_KEY, mode);
  } catch (error) {
    console.warn('[appearance] could not save the appearance', error);
  }
}

/**
 * Loads the stored appearance, or `null` when the user has never chosen one.
 *
 * A missing key falls back to the pre-KNOT-020k `knot.theme.v1` value once, so an
 * existing explicit choice is preserved. An unreadable store, an unreadable legacy
 * value, and an unknown value all resolve to `null`, which the provider reads as
 * "follow the system" — the caller never has to distinguish the cases.
 */
export async function loadAppearance(): Promise<AppearanceMode | null> {
  let raw: string | null;

  try {
    raw = await AsyncStorage.getItem(APPEARANCE_KEY);
  } catch (error) {
    console.warn('[appearance] could not read the stored appearance', error);
    return null;
  }

  if (raw !== null) {
    if (!isAppearanceMode(raw)) {
      console.warn('[appearance] stored appearance is not a known mode; following the system');
      return null;
    }
    return raw;
  }

  // No appearance yet: adopt a pre-KNOT-020k explicit choice if there is one.
  const legacy = await loadTheme();
  return legacy === 'light' || legacy === 'dark' ? legacy : null;
}

/** Removes the stored appearance, so the app follows the system again. */
export async function clearAppearance(): Promise<void> {
  try {
    await AsyncStorage.removeItem(APPEARANCE_KEY);
  } catch (error) {
    console.warn('[appearance] could not clear the appearance', error);
  }
}

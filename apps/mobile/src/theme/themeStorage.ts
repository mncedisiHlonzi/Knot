/**
 * Theme preference persistence for the Knot mobile app.
 *
 * The theme is a device preference, not an account setting: it lives in
 * AsyncStorage beside the session, under its own versioned key, so switching
 * device or signing out does not move it (KNOT-ADR-077).
 *
 * Storage is best-effort throughout, exactly as `session.ts` treats it: a
 * failure is logged and swallowed, and a missing or corrupted value simply means
 * "no stored preference", which lets the app fall back to the system theme. The
 * app must never crash — or refuse to start — because storage was unavailable.
 */
import AsyncStorage from '@react-native-async-storage/async-storage';

import type { ThemeName } from './colors';

/**
 * The AsyncStorage key the theme is stored under.
 *
 * The `.v1` suffix versions the stored value, so a future change to the set of
 * themes can migrate (or ignore) an old value instead of misreading it.
 */
export const THEME_KEY = 'knot.theme.v1';

/** Reports whether a parsed value is one of the themes we know. */
function isThemeName(value: unknown): value is ThemeName {
  return value === 'light' || value === 'dark';
}

/**
 * Persists the chosen theme.
 *
 * A storage failure is logged and swallowed: the choice still applies for this
 * run, it simply will not survive a restart.
 */
export async function saveTheme(theme: ThemeName): Promise<void> {
  try {
    await AsyncStorage.setItem(THEME_KEY, theme);
  } catch (error) {
    console.warn('[theme] could not save the theme', error);
  }
}

/**
 * Loads the stored theme, or `null` when the user has never chosen one.
 *
 * A missing key, unreadable storage, and an unknown value all resolve to `null`
 * (each logged), which the provider reads as "follow the system". The caller
 * therefore never has to distinguish "no preference" from "unusable preference".
 */
export async function loadTheme(): Promise<ThemeName | null> {
  let raw: string | null;

  try {
    raw = await AsyncStorage.getItem(THEME_KEY);
  } catch (error) {
    console.warn('[theme] could not read the stored theme', error);
    return null;
  }

  if (raw === null) {
    return null;
  }

  if (!isThemeName(raw)) {
    console.warn('[theme] stored theme is not a known theme; ignoring it');
    return null;
  }

  return raw;
}

/**
 * Removes the stored theme, so the app follows the system again.
 *
 * Not used by the UI today — the toggle always writes a choice — but it keeps
 * the module's contract symmetric with `session.ts` and gives a future "follow
 * the system" option a way back.
 */
export async function clearTheme(): Promise<void> {
  try {
    await AsyncStorage.removeItem(THEME_KEY);
  } catch (error) {
    console.warn('[theme] could not clear the stored theme', error);
  }
}

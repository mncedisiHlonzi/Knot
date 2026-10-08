/**
 * Session persistence for the Knot mobile app.
 *
 * The signed-in session previously lived only in React state, so closing the app
 * lost it and every restart meant registering again. It is now written to
 * AsyncStorage under a single versioned key and restored on launch (KNOT-ADR-024).
 *
 * Storage is treated as best-effort throughout: any failure is logged and
 * swallowed, and a missing or corrupted value simply means "not signed in". The
 * app must never crash because storage was unavailable or held junk — the worst
 * outcome is a return to the login screen.
 *
 * This is deliberately plain AsyncStorage, not secure storage. AsyncStorage is
 * unencrypted; the tokens it holds are short-lived development credentials for an
 * MVP with no sensitive data yet. Moving to encrypted storage (Keychain/Keystore)
 * before handling anything sensitive is tracked in KNOT-ADR-024.
 */
import AsyncStorage from '@react-native-async-storage/async-storage';

import type { User } from '../api/client';

/** The signed-in session: the two tokens plus the profile they belong to. */
export type Session = {
  readonly accessToken: string;
  readonly refreshToken: string;
  readonly user: User;
};

/**
 * The AsyncStorage key the session is stored under.
 *
 * The `.v1` suffix is deliberate: it versions the stored shape so a future
 * change to `Session` can migrate (or ignore) an old value instead of crashing on
 * it.
 */
export const SESSION_KEY = 'knot.session.v1';

/**
 * Reports whether a parsed value is a well-formed Session.
 *
 * This guards against a value that is valid JSON but not a session (e.g. a
 * half-written object from a previous version), which a bare parse would happily
 * hand back as a Session and then fail far away when a token was read.
 */
function isSession(value: unknown): value is Session {
  if (typeof value !== 'object' || value === null) {
    return false;
  }

  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.accessToken === 'string' &&
    candidate.accessToken !== '' &&
    typeof candidate.refreshToken === 'string' &&
    candidate.refreshToken !== '' &&
    typeof candidate.user === 'object' &&
    candidate.user !== null
  );
}

/**
 * Persists the session, serialised as JSON, under {@link SESSION_KEY}.
 *
 * A storage failure is logged and swallowed: the in-memory session still works
 * for this run, it simply will not survive a restart.
 */
export async function saveSession(session: Session): Promise<void> {
  try {
    await AsyncStorage.setItem(SESSION_KEY, JSON.stringify(session));
  } catch (error) {
    console.warn('[session] could not save the session', error);
  }
}

/**
 * Loads the stored session, or `null` when there is none.
 *
 * A missing key, unreadable storage, malformed JSON, and a value that is not a
 * Session all resolve to `null` (each logged). The caller therefore never has to
 * distinguish "no session" from "unusable session" — both mean "show the login
 * screen".
 */
export async function loadSession(): Promise<Session | null> {
  let raw: string | null;

  try {
    raw = await AsyncStorage.getItem(SESSION_KEY);
  } catch (error) {
    console.warn('[session] could not read the stored session', error);
    return null;
  }

  if (raw === null) {
    return null;
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (error) {
    console.warn('[session] stored session is not valid JSON; ignoring it', error);
    return null;
  }

  if (!isSession(parsed)) {
    console.warn('[session] stored session is not a valid session; ignoring it');
    return null;
  }

  return parsed;
}

/**
 * Removes the stored session. Called on sign-out.
 *
 * A storage failure is logged and swallowed; the caller clears its in-memory
 * state regardless, so the user is signed out from the app's point of view even
 * if the stored copy could not be deleted.
 */
export async function clearSession(): Promise<void> {
  try {
    await AsyncStorage.removeItem(SESSION_KEY);
  } catch (error) {
    console.warn('[session] could not clear the stored session', error);
  }
}

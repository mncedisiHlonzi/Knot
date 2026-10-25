/**
 * Session persistence for the Knot mobile app.
 *
 * The **refresh token is the only secret that touches storage.** The access token
 * lives in memory for the 15 minutes it is valid: it is short-lived, it is
 * replaced by every refresh, and writing it to disk would create a second copy of a
 * credential that does not need one (KNOT-ADR-080).
 *
 * Storage is treated as best-effort throughout: any failure is logged and
 * swallowed, and a missing or corrupted value simply means "not signed in". The app
 * must never crash because storage was unavailable or held junk — the worst
 * outcome is a return to the login screen.
 *
 * This is deliberately plain AsyncStorage, not secure storage. AsyncStorage is
 * unencrypted; a follow-up task moves the refresh token to Keychain/Keystore
 * (KNOT-ADR-024).
 *
 * ## Why the session is restored with a refresh
 *
 * An access token in memory does not survive the process, and one on disk would be
 * expired by the time it was read. So a stored session is state that can *obtain* a
 * session: on launch the refresh token is spent, rotated, and the pair is rebuilt.
 * That also means a session revoked while the app was closed cannot come back.
 */
import AsyncStorage from '@react-native-async-storage/async-storage';

import { refresh as refreshSession } from '../api/auth';
import { ApiError, configureSessionBridge, type AuthResponse, type User } from '../api/client';

/** The signed-in session as the app holds it in memory. */
export type Session = {
  readonly accessToken: string;
  readonly refreshToken: string;
  readonly user: User;
};

/**
 * What is written to storage: the refresh token and the profile it belongs to.
 *
 * The user is stored so the app can render a name before the refresh round trip
 * finishes, and so a network failure on launch does not lose the display name.
 */
type StoredSession = {
  readonly refreshToken: string;
  readonly user: User;
};

/**
 * The AsyncStorage key the session is stored under.
 *
 * The `.v2` suffix marks the shape change: v1 held both tokens in one object. A
 * stored v1 value is cleared on first launch rather than migrated, because its
 * access token is expired and its refresh token belonged to the pre-session backend
 * and cannot be used at all.
 */
export const SESSION_KEY = 'knot.session.v2';

/** The key KNOT-024 wrote, cleared once on first launch of a v2 build. */
export const LEGACY_SESSION_KEY = 'knot.session.v1';

/**
 * The in-memory session.
 *
 * It is module state rather than React state because the HTTP client needs the
 * access token outside the component tree (a request can outlive the screen that
 * started it), and because exactly the same value has to be readable synchronously
 * while a render is happening.
 */
let current: Session | null = null;

/**
 * The functions to call when a session ends without the person asking.
 *
 * It exists because the event starts outside React: a request fails, the refresh is
 * refused, and the session is over. Without this, the app would keep rendering the
 * signed-in tree with no credential behind it — every subsequent request failing —
 * instead of returning to the login screen.
 */
const sessionExpiryListeners = new Set<() => void>();

/**
 * Registers a listener for an expired session, and returns the unsubscribe.
 *
 * `App` uses it to drop the session and route to login.
 */
export function subscribeToSessionExpiry(listener: () => void): () => void {
  sessionExpiryListeners.add(listener);

  return () => {
    sessionExpiryListeners.delete(listener);
  };
}

/** Tells every listener the session is over. */
function notifySessionExpired(): void {
  for (const listener of sessionExpiryListeners) {
    listener();
  }
}

/**
 * Reports whether a parsed value is a well-formed stored session.
 *
 * This guards against a value that is valid JSON but not a session (for example a
 * half-written object), which a bare parse would happily hand back and then fail
 * far away when a token was read.
 */
function isStoredSession(value: unknown): value is StoredSession {
  if (typeof value !== 'object' || value === null) {
    return false;
  }

  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.refreshToken === 'string' &&
    candidate.refreshToken !== '' &&
    typeof candidate.user === 'object' &&
    candidate.user !== null
  );
}

/** Returns the in-memory session, or null when nobody is signed in. */
export function getSession(): Session | null {
  return current;
}

/** Returns the in-memory access token, or null when nobody is signed in. */
export function getAccessToken(): string | null {
  return current?.accessToken ?? null;
}

/** Returns the refresh token that can obtain a new access token, if any. */
export function getRefreshToken(): string | null {
  return current?.refreshToken ?? null;
}

/**
 * Replaces the in-memory tokens after a refresh.
 *
 * The user is kept from the previous value when the response omits it, so a refresh
 * can never blank out the profile the app is rendering.
 */
function applyRefreshedTokens(result: AuthResponse): void {
  const previous = current;
  const next: Session = {
    accessToken: result.access_token,
    refreshToken: result.refresh_token,
    user: result.user ?? previous?.user ?? ({} as User),
  };

  current = next;
  void persist(next);
}

/**
 * Installs the bridge the HTTP client uses to refresh.
 *
 * It is called at module load rather than from `App.tsx`: every module that can
 * make a request imports this one transitively, so registering here guarantees the
 * client is never used without a way to refresh, and removes an ordering rule
 * nobody would remember.
 */
function installBridge(): void {
  configureSessionBridge({
    getRefreshToken,
    onTokensRefreshed: applyRefreshedTokens,
    onSessionExpired: () => {
      // The server refused the refresh token outright: the session is over, so the
      // stored copy goes and the app is told to show the login screen.
      void clearSession();
      notifySessionExpired();
    },
  });
}

installBridge();

/** Writes the refresh token and user to storage, swallowing failures. */
async function persist(session: Session): Promise<void> {
  const stored: StoredSession = { refreshToken: session.refreshToken, user: session.user };

  try {
    await AsyncStorage.setItem(SESSION_KEY, JSON.stringify(stored));
  } catch (error) {
    console.warn('[session] could not save the session', error);
  }
}

/**
 * Records a freshly issued session: in memory for the access token, in storage for
 * the refresh token.
 *
 * A storage failure is logged and swallowed: the session still works for this run,
 * it simply will not survive a restart.
 */
export async function saveSession(session: Session): Promise<void> {
  current = session;
  await persist(session);
}

/**
 * Removes the stored session and forgets the in-memory one.
 *
 * Called on sign-out, and when the server refuses a refresh. A storage failure is
 * logged and swallowed; the app clears its in-memory state regardless, so the user
 * is signed out from the app's point of view even if the stored copy could not be
 * deleted.
 */
export async function clearSession(): Promise<void> {
  current = null;

  try {
    await AsyncStorage.removeItem(SESSION_KEY);
  } catch (error) {
    console.warn('[session] could not clear the stored session', error);
  }
}

/**
 * Removes a session stored by the previous shape.
 *
 * KNOT-024's value held an access token and a refresh token issued before sessions
 * existed. Neither is usable, and leaving it would make a downgrade attempt read
 * half a session, so it is deleted once and then never looked for again.
 */
async function clearLegacySession(): Promise<void> {
  try {
    const legacy = await AsyncStorage.getItem(LEGACY_SESSION_KEY);
    if (legacy !== null) {
      await AsyncStorage.removeItem(LEGACY_SESSION_KEY);
      console.warn('[session] cleared a pre-KNOT-031 session; sign in again');
    }
  } catch (error) {
    console.warn('[session] could not clear the old stored session', error);
  }
}

/** Reads the stored session, treating anything unusable as "none". */
async function readStoredSession(): Promise<StoredSession | null> {
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

  if (!isStoredSession(parsed)) {
    console.warn('[session] stored session is not a valid session; ignoring it');
    return null;
  }

  return parsed;
}

/**
 * Restores the stored session by spending its refresh token, or returns null.
 *
 * Three outcomes, deliberately different:
 *
 *  - **No stored session** — null, quietly. A first launch is not an error.
 *  - **The server refuses the refresh token (`401`)** — the session is over: the
 *    stored copy is cleared and null is returned, so the app shows the login screen.
 *  - **The refresh could not be attempted (offline, 5xx)** — null is returned but
 *    the stored token is kept, because being offline is not being signed out and
 *    the next launch may well succeed.
 */
export async function loadSession(): Promise<Session | null> {
  await clearLegacySession();

  const stored = await readStoredSession();
  if (stored === null) {
    return null;
  }

  try {
    const result = await refreshSession(stored.refreshToken);
    const session: Session = {
      accessToken: result.access_token,
      refreshToken: result.refresh_token,
      user: result.user ?? stored.user,
    };

    await saveSession(session);

    return session;
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      await clearSession();
      return null;
    }

    console.warn(
      '[session] could not restore the session; keeping the token for next launch',
      error,
    );
    return null;
  }
}

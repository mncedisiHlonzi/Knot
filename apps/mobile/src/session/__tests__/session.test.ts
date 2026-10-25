import AsyncStorage from '@react-native-async-storage/async-storage';

import { ApiError, type AuthResponse } from '../../api/client';
import { refresh as refreshApi } from '../../api/auth';
import { refreshAccessToken } from '../../api/client';
import {
  LEGACY_SESSION_KEY,
  SESSION_KEY,
  type Session,
  clearSession,
  getAccessToken,
  getRefreshToken,
  loadSession,
  saveSession,
  subscribeToSessionExpiry,
} from '../session';

// Use the mock the package ships, so the tests exercise the module's logic over
// an in-memory store rather than any native module.
jest.mock('@react-native-async-storage/async-storage', () =>
  jest.requireActual('@react-native-async-storage/async-storage/jest/async-storage-mock'),
);

// The refresh call is the module's only network dependency, and a real fetch in a
// unit test would be a network assertion rather than a logic one.
jest.mock('../../api/auth', () => ({ refresh: jest.fn() }));

const mockedRefresh = refreshApi as jest.MockedFunction<typeof refreshApi>;

const USER = {
  id: '11111111-1111-1111-1111-111111111111',
  email: 'teller@example.com',
  display_name: 'Teller',
  preferred_languages: ['eng'],
  approximate_location: 'Cape Town',
  phone: '',
  created_at: '2026-10-09T00:00:00Z',
  email_verified_at: '2026-10-09T00:00:01Z',
  avatar_url: '',
};

const SESSION: Session = {
  accessToken: 'access-token',
  refreshToken: 'session-id.secret',
  user: USER,
};

/** The body a successful refresh returns. */
function refreshed(overrides: Partial<AuthResponse> = {}): AuthResponse {
  return {
    access_token: 'new-access-token',
    refresh_token: 'session-id.new-secret',
    expires_in: 900,
    user: USER,
    ...overrides,
  };
}

describe('session storage', () => {
  beforeEach(async () => {
    jest.restoreAllMocks();
    jest.clearAllMocks();
    await AsyncStorage.clear();
    await clearSession();
  });

  it('persists only the refresh token, never the access token', async () => {
    await saveSession(SESSION);

    const raw = await AsyncStorage.getItem(SESSION_KEY);
    expect(raw).not.toBeNull();

    const stored = JSON.parse(raw ?? '{}') as Record<string, unknown>;
    expect(stored.refreshToken).toBe(SESSION.refreshToken);
    expect(stored.user).toMatchObject({ id: USER.id });
    // The access token is short-lived and stays in memory (KNOT-ADR-080).
    expect(stored.accessToken).toBeUndefined();
    expect(raw).not.toContain('access-token');
  });

  it('keeps the access token in memory for the running app', async () => {
    await saveSession(SESSION);

    expect(getAccessToken()).toBe('access-token');
    expect(getRefreshToken()).toBe('session-id.secret');
  });

  it('loads a session by spending the stored refresh token', async () => {
    mockedRefresh.mockResolvedValueOnce(refreshed());
    await saveSession(SESSION);

    const restored = await loadSession();

    expect(mockedRefresh).toHaveBeenCalledWith(SESSION.refreshToken);
    expect(restored).toEqual({
      accessToken: 'new-access-token',
      refreshToken: 'session-id.new-secret',
      user: USER,
    });
    // The rotated token is what gets stored, so the next launch can restore again.
    expect(await AsyncStorage.getItem(SESSION_KEY)).toContain('session-id.new-secret');
  });

  it('returns null when nothing has been stored', async () => {
    await expect(loadSession()).resolves.toBeNull();
    expect(mockedRefresh).not.toHaveBeenCalled();
  });

  it('returns null and forgets the session when the server refuses the token', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    mockedRefresh.mockRejectedValueOnce(
      new ApiError('sign in again', 401, 'invalid_refresh_token'),
    );
    await AsyncStorage.setItem(
      SESSION_KEY,
      JSON.stringify({ refreshToken: 'stale.secret', user: USER }),
    );

    await expect(loadSession()).resolves.toBeNull();
    // A refused token is a session that is over, so the stored copy goes too.
    await expect(AsyncStorage.getItem(SESSION_KEY)).resolves.toBeNull();
  });

  it('keeps the stored token when the refresh could not be attempted', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    mockedRefresh.mockRejectedValueOnce(new ApiError('offline', 0, 'network_error'));
    await AsyncStorage.setItem(
      SESSION_KEY,
      JSON.stringify({ refreshToken: 'kept.secret', user: USER }),
    );

    await expect(loadSession()).resolves.toBeNull();
    // Being offline is not being signed out: the token survives for the next launch.
    expect(await AsyncStorage.getItem(SESSION_KEY)).toContain('kept.secret');
  });

  it('clears a session stored by the previous shape', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    await AsyncStorage.setItem(LEGACY_SESSION_KEY, JSON.stringify({ accessToken: 'old' }));

    await expect(loadSession()).resolves.toBeNull();

    await expect(AsyncStorage.getItem(LEGACY_SESSION_KEY)).resolves.toBeNull();
    expect(mockedRefresh).not.toHaveBeenCalled();
  });

  it('returns null when the stored value is corrupted JSON', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    await AsyncStorage.setItem(SESSION_KEY, '{ this is not json');

    await expect(loadSession()).resolves.toBeNull();
  });

  it('returns null when the stored value is not a valid session', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    await AsyncStorage.setItem(SESSION_KEY, JSON.stringify({ unexpected: true }));

    await expect(loadSession()).resolves.toBeNull();
  });

  it('clears the stored session and forgets it in memory', async () => {
    await saveSession(SESSION);

    await clearSession();

    expect(getAccessToken()).toBeNull();
    expect(getRefreshToken()).toBeNull();
    await expect(AsyncStorage.getItem(SESSION_KEY)).resolves.toBeNull();
  });

  it('never throws when storage fails', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);

    jest.spyOn(AsyncStorage, 'setItem').mockRejectedValueOnce(new Error('storage full'));
    await expect(saveSession(SESSION)).resolves.toBeUndefined();

    jest.spyOn(AsyncStorage, 'getItem').mockRejectedValueOnce(new Error('storage full'));
    await expect(loadSession()).resolves.toBeNull();

    jest.spyOn(AsyncStorage, 'removeItem').mockRejectedValueOnce(new Error('storage full'));
    await expect(clearSession()).resolves.toBeUndefined();
  });
});

describe('session expiry', () => {
  beforeEach(async () => {
    jest.restoreAllMocks();
    jest.clearAllMocks();
    await AsyncStorage.clear();
    await clearSession();
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('tells the app the session is over when the refresh is refused', async () => {
    // A refused refresh is the moment the session ends, and it happens inside the
    // HTTP client — outside React. The app subscribes so it can route to login
    // instead of rendering a signed-in tree with no credential behind it.
    await saveSession(SESSION);

    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: false,
        status: 401,
        text: () => Promise.resolve('{"error":{"code":"invalid_refresh_token"}}'),
        json: () => Promise.resolve({ error: { code: 'invalid_refresh_token' } }),
      } as unknown as Response),
    ) as unknown as typeof fetch;

    const listener = jest.fn();
    const unsubscribe = subscribeToSessionExpiry(listener);

    await expect(refreshAccessToken()).resolves.toBeNull();

    expect(listener).toHaveBeenCalledTimes(1);
    expect(getAccessToken()).toBeNull();
    await expect(AsyncStorage.getItem(SESSION_KEY)).resolves.toBeNull();

    unsubscribe();
  });

  it('stops listening once unsubscribed', async () => {
    const listener = jest.fn();
    const unsubscribe = subscribeToSessionExpiry(listener);
    unsubscribe();

    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: false,
        status: 401,
        text: () => Promise.resolve('{}'),
        json: () => Promise.resolve({}),
      } as unknown as Response),
    ) as unknown as typeof fetch;

    await saveSession(SESSION);
    await refreshAccessToken();

    expect(listener).not.toHaveBeenCalled();
  });
});

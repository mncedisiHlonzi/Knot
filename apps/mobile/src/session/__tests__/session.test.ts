import AsyncStorage from '@react-native-async-storage/async-storage';

import { SESSION_KEY, Session, clearSession, loadSession, saveSession } from '../session';

// Use the mock the package ships, so the tests exercise the module's logic over
// an in-memory store rather than any native module.
jest.mock('@react-native-async-storage/async-storage', () =>
  jest.requireActual('@react-native-async-storage/async-storage/jest/async-storage-mock'),
);

const SESSION: Session = {
  accessToken: 'access-token',
  refreshToken: 'refresh-token',
  user: {
    id: '11111111-1111-1111-1111-111111111111',
    email: 'teller@example.com',
    display_name: 'Teller',
    preferred_languages: ['en'],
    approximate_location: 'Cape Town',
    phone: '',
    created_at: '2026-10-09T00:00:00Z',
    avatar_url: '',
  },
};

describe('session storage', () => {
  beforeEach(async () => {
    jest.restoreAllMocks();
    await AsyncStorage.clear();
  });

  it('saves a session and loads the same session back', async () => {
    await saveSession(SESSION);

    await expect(loadSession()).resolves.toEqual(SESSION);
  });

  it('returns null when nothing has been stored', async () => {
    await expect(loadSession()).resolves.toBeNull();
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

  it('clears the stored session', async () => {
    await saveSession(SESSION);

    await clearSession();

    await expect(loadSession()).resolves.toBeNull();
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

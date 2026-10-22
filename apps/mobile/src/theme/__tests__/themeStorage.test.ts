import AsyncStorage from '@react-native-async-storage/async-storage';

import { THEME_KEY, clearTheme, loadTheme, saveTheme } from '../themeStorage';

// Use the mock the package ships, so the tests exercise this module's logic over
// an in-memory store rather than any native module.
jest.mock('@react-native-async-storage/async-storage', () =>
  jest.requireActual('@react-native-async-storage/async-storage/jest/async-storage-mock'),
);

describe('theme storage', () => {
  beforeEach(async () => {
    jest.restoreAllMocks();
    await AsyncStorage.clear();
  });

  it('saves a theme and loads the same theme back', async () => {
    await saveTheme('dark');

    await expect(loadTheme()).resolves.toBe('dark');
  });

  it('stores the theme under the versioned key', async () => {
    await saveTheme('light');

    await expect(AsyncStorage.getItem(THEME_KEY)).resolves.toBe('light');
    expect(THEME_KEY).toBe('knot.theme.v1');
  });

  it('returns null when no choice has been stored, so the app follows the system', async () => {
    await expect(loadTheme()).resolves.toBeNull();
  });

  it('ignores a stored value that is not a theme', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    await AsyncStorage.setItem(THEME_KEY, 'solarized');

    await expect(loadTheme()).resolves.toBeNull();
    expect(warn).toHaveBeenCalled();
  });

  it('treats unreadable storage as no stored choice', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    jest.spyOn(AsyncStorage, 'getItem').mockRejectedValueOnce(new Error('no storage'));

    await expect(loadTheme()).resolves.toBeNull();
    expect(warn).toHaveBeenCalled();
  });

  it('swallows a save failure, because the choice still applies for this run', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    jest.spyOn(AsyncStorage, 'setItem').mockRejectedValueOnce(new Error('no storage'));

    await expect(saveTheme('dark')).resolves.toBeUndefined();
    expect(warn).toHaveBeenCalled();
  });

  it('clears the stored choice, so the app follows the system again', async () => {
    await saveTheme('dark');

    await clearTheme();

    await expect(loadTheme()).resolves.toBeNull();
  });

  it('swallows a clear failure', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    jest.spyOn(AsyncStorage, 'removeItem').mockRejectedValueOnce(new Error('no storage'));

    await expect(clearTheme()).resolves.toBeUndefined();
    expect(warn).toHaveBeenCalled();
  });
});

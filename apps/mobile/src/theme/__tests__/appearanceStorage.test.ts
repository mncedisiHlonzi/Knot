import AsyncStorage from '@react-native-async-storage/async-storage';

import {
  APPEARANCE_KEY,
  clearAppearance,
  loadAppearance,
  saveAppearance,
} from '../appearanceStorage';
import { loadTheme } from '../themeStorage';

/**
 * The appearance store is the three-value successor of `knot.theme.v1`
 * (KNOT-020k). These tests pin the key, the values, and the one-time migration so
 * a reader who had chosen dark before this task keeps it.
 */
jest.mock('@react-native-async-storage/async-storage', () => ({
  __esModule: true,
  default: {
    getItem: jest.fn(async () => null),
    setItem: jest.fn(async () => undefined),
    removeItem: jest.fn(async () => undefined),
  },
}));

jest.mock('../themeStorage', () => ({
  loadTheme: jest.fn(async () => null),
}));

const storage = AsyncStorage as unknown as {
  getItem: jest.Mock;
  setItem: jest.Mock;
  removeItem: jest.Mock;
};

describe('appearance storage', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    storage.getItem.mockResolvedValue(null);
    jest.mocked(loadTheme).mockResolvedValue(null);
  });

  it('stores the mode under the versioned appearance key', async () => {
    await saveAppearance('dark');

    expect(APPEARANCE_KEY).toBe('knot.appearance.v1');
    expect(storage.setItem).toHaveBeenCalledWith('knot.appearance.v1', 'dark');
  });

  it('reads back each of the three modes', async () => {
    for (const mode of ['system', 'light', 'dark'] as const) {
      storage.getItem.mockResolvedValueOnce(mode);
      await expect(loadAppearance()).resolves.toBe(mode);
    }
  });

  it('adopts a pre-KNOT-020k theme choice once, when no appearance is stored', async () => {
    jest.mocked(loadTheme).mockResolvedValue('dark');

    await expect(loadAppearance()).resolves.toBe('dark');
    expect(loadTheme).toHaveBeenCalledTimes(1);
  });

  it('does not consult the legacy key when an appearance exists', async () => {
    storage.getItem.mockResolvedValue('light');
    jest.mocked(loadTheme).mockResolvedValue('dark');

    await expect(loadAppearance()).resolves.toBe('light');
    expect(loadTheme).not.toHaveBeenCalled();
  });

  it('follows the system when the stored value is unknown', async () => {
    storage.getItem.mockResolvedValue('purple');

    await expect(loadAppearance()).resolves.toBeNull();
  });

  it('follows the system when storage cannot be read', async () => {
    storage.getItem.mockRejectedValue(new Error('unavailable'));

    await expect(loadAppearance()).resolves.toBeNull();
  });

  it('swallows a write failure so the choice still applies for the run', async () => {
    storage.setItem.mockRejectedValue(new Error('unavailable'));

    await expect(saveAppearance('light')).resolves.toBeUndefined();
  });

  it('clears the stored appearance', async () => {
    await clearAppearance();

    expect(storage.removeItem).toHaveBeenCalledWith('knot.appearance.v1');
  });
});

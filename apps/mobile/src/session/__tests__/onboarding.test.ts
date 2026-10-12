import AsyncStorage from '@react-native-async-storage/async-storage';

import { ONBOARDING_KEY, hasSeenOnboarding, markOnboardingSeen } from '../onboarding';

// The real AsyncStorage is a native module; the package ships a Jest mock.
jest.mock('@react-native-async-storage/async-storage', () =>
  jest.requireActual('@react-native-async-storage/async-storage/jest/async-storage-mock'),
);

describe('onboarding flag storage', () => {
  beforeEach(async () => {
    await AsyncStorage.clear();
    jest.restoreAllMocks();
  });

  it('reports false when nothing has been stored', async () => {
    await expect(hasSeenOnboarding()).resolves.toBe(false);
  });

  it('persists and reads back the flag', async () => {
    await markOnboardingSeen();
    expect(await AsyncStorage.getItem(ONBOARDING_KEY)).toBe('true');
    await expect(hasSeenOnboarding()).resolves.toBe(true);
  });

  it('treats a non-marker value as not seen', async () => {
    await AsyncStorage.setItem(ONBOARDING_KEY, 'maybe');
    await expect(hasSeenOnboarding()).resolves.toBe(false);
  });

  it('never throws when storage fails', async () => {
    jest.spyOn(AsyncStorage, 'getItem').mockRejectedValueOnce(new Error('storage unavailable'));
    await expect(hasSeenOnboarding()).resolves.toBe(false);

    jest.spyOn(AsyncStorage, 'setItem').mockRejectedValueOnce(new Error('storage full'));
    await expect(markOnboardingSeen()).resolves.toBeUndefined();
  });
});

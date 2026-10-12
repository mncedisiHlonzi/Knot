/**
 * First-run flag persistence (KNOT-020c).
 *
 * Onboarding is shown once. The flag lives in AsyncStorage under its own
 * versioned key, mirroring the session storage in `session.ts` — the app's one
 * persistence module today (there is no separate KNOT-009 storage module). Like
 * the session, storage is treated as best-effort: a failure is logged and
 * swallowed, and the worst outcome is that onboarding is shown again.
 */
import AsyncStorage from '@react-native-async-storage/async-storage';

/**
 * The AsyncStorage key the first-run flag is stored under. The `.v1` suffix
 * versions the stored value so a future change can migrate it.
 */
export const ONBOARDING_KEY = 'knot.onboarding.v1';

/**
 * Reports whether onboarding has been completed on this device.
 *
 * A missing key, unreadable storage, or any value other than the exact marker
 * resolves to `false`, so an unusable value means "show onboarding again" rather
 * than skipping it.
 */
export async function hasSeenOnboarding(): Promise<boolean> {
  try {
    const stored = await AsyncStorage.getItem(ONBOARDING_KEY);
    return stored === 'true';
  } catch (error) {
    console.warn('[onboarding] could not read the onboarding flag', error);
    return false;
  }
}

/**
 * Records that onboarding has been completed. A storage failure is logged and
 * swallowed: the in-memory flow still proceeds, it simply will not be remembered
 * across a restart.
 */
export async function markOnboardingSeen(): Promise<void> {
  try {
    await AsyncStorage.setItem(ONBOARDING_KEY, 'true');
  } catch (error) {
    console.warn('[onboarding] could not save the onboarding flag', error);
  }
}

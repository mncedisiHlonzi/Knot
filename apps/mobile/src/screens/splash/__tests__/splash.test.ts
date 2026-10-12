import { SPLASH_DURATION_MS, postSplashDestination } from '../splash';

describe('splash logic', () => {
  it('keeps the splash on screen for a short, fixed time', () => {
    expect(SPLASH_DURATION_MS).toBeGreaterThan(0);
    expect(SPLASH_DURATION_MS).toBeLessThanOrEqual(2000);
  });

  it('sends a signed-in user straight past the boot flow', () => {
    // A session always wins: onboarding ends at the auth screens, so a signed-in
    // user has necessarily been onboarded already.
    expect(postSplashDestination(true, true)).toBe('app');
    expect(postSplashDestination(true, false)).toBe('app');
  });

  it('shows onboarding on a first run with no session', () => {
    expect(postSplashDestination(false, false)).toBe('onboarding');
  });

  it('goes to the auth screens once onboarding has been seen', () => {
    expect(postSplashDestination(false, true)).toBe('app');
  });
});

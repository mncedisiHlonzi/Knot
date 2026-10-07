import { APP_NAME, APP_VERSION, appMetadata } from '../src/config/app';

describe('app config', () => {
  it('exposes the Knot working name', () => {
    expect(APP_NAME).toBe('Knot');
  });

  it('exposes a semantic version', () => {
    expect(APP_VERSION).toMatch(/^\d+\.\d+\.\d+$/);
  });

  it('builds a complete metadata object', () => {
    expect(appMetadata.name).toBe(APP_NAME);
    expect(appMetadata.version).toBe(APP_VERSION);
  });
});

import { describeDevice, lastUsedLabel } from '../sessionFormat';

/** The device label a Sessions row shows. */
describe('describeDevice', () => {
  it('names the platform and the app version', () => {
    expect(describeDevice({ platform: 'ios', app_version: '0.1.0' })).toBe('ios · 0.1.0');
  });

  it('shows a platform alone when no version was sent', () => {
    expect(describeDevice({ platform: 'android' })).toBe('android');
  });

  it('shows a version alone when no platform was sent', () => {
    expect(describeDevice({ app_version: '0.1.0' })).toBe('Version 0.1.0');
  });

  it('falls back to a neutral label for anything unusable', () => {
    expect(describeDevice({})).toBe('Unknown device');
    expect(describeDevice(null)).toBe('Unknown device');
    expect(describeDevice('ios')).toBe('Unknown device');
    expect(describeDevice({ platform: 42, app_version: null })).toBe('Unknown device');
  });

  it('trims what the client sent', () => {
    expect(describeDevice({ platform: '  ios  ', app_version: ' 0.1.0 ' })).toBe('ios · 0.1.0');
  });
});

describe('lastUsedLabel', () => {
  const now = new Date('2026-10-26T12:00:00Z');

  it('calls the current moment "Active now"', () => {
    expect(lastUsedLabel('2026-10-26T11:59:50Z', now)).toBe('Active now');
  });

  it('describes older use relatively', () => {
    expect(lastUsedLabel('2026-10-26T11:55:00Z', now)).toBe('Last used 5 minutes ago');
    expect(lastUsedLabel('2026-10-26T09:00:00Z', now)).toBe('Last used 3 hours ago');
    expect(lastUsedLabel('2026-10-24T12:00:00Z', now)).toBe('Last used 2 days ago');
  });

  it('returns an unparseable timestamp unchanged rather than saying "Invalid Date"', () => {
    expect(lastUsedLabel('not-a-time', now)).toBe('Last used not-a-time');
  });
});

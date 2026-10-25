/**
 * Pure formatting for the Sessions screen.
 *
 * A device row is two strings: what the device is, and when it was last used. The
 * "when" reuses `formatRelativeTime` so a session reads the same way as every other
 * timestamp in the app; only the device label is new here.
 */
import { formatRelativeTime } from './time';

/**
 * Returns a short human label for a session's device.
 *
 * The server stores whatever the client chose to send in `device_info`, so this
 * reads defensively: `platform` and `app_version` are used when they are strings,
 * an empty object or an unknown shape falls back to "Unknown device", and a
 * platform alone is shown alone rather than as "ios · ".
 */
export function describeDevice(deviceInfo: unknown): string {
  if (typeof deviceInfo !== 'object' || deviceInfo === null) {
    return 'Unknown device';
  }

  const record = deviceInfo as Record<string, unknown>;
  const platform = typeof record.platform === 'string' ? record.platform.trim() : '';
  const version = typeof record.app_version === 'string' ? record.app_version.trim() : '';

  if (platform === '' && version === '') {
    return 'Unknown device';
  }
  if (version === '') {
    return platform;
  }
  if (platform === '') {
    return `Version ${version}`;
  }

  return `${platform} · ${version}`;
}

/**
 * Returns the "last used" line for a session row.
 *
 * "just now" becomes "Active now" — a device list that says "just now" for the
 * session the person is reading it on reads oddly, and the distinction (this one is
 * in use) is exactly what the line is for.
 */
export function lastUsedLabel(lastUsedAt: string, now: Date = new Date()): string {
  const phrase = formatRelativeTime(lastUsedAt, now);

  return phrase === 'just now' ? 'Active now' : `Last used ${phrase}`;
}

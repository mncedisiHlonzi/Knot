/**
 * Time formatting for content timestamps.
 *
 * It lives in one place so every screen describes "when" the same way, and so the
 * wording can be tested without rendering a screen. It is deliberately a plain
 * function of its inputs: the "now" it compares against is a parameter, so a test
 * never depends on the wall clock.
 */

/** One second, one minute, and one hour in milliseconds. */
const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;

/**
 * Formats an ISO-8601 timestamp as a short relative phrase such as "just now",
 * "3 minutes ago", or "2 weeks ago".
 *
 * A timestamp that cannot be parsed is returned verbatim rather than as
 * "Invalid Date", so a server-side format change is visible instead of confusing.
 * A timestamp in the future (a clock skew between the device and the server)
 * reads as "just now" rather than as a negative age.
 *
 * @param value An ISO-8601 timestamp, as the API returns them.
 * @param now The instant to compare against. Defaults to the current time.
 */
export function formatRelativeTime(value: string, now: Date = new Date()): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }

  const elapsed = now.getTime() - parsed.getTime();
  if (elapsed < 45 * SECOND) {
    return 'just now';
  }
  if (elapsed < 90 * SECOND) {
    return '1 minute ago';
  }
  if (elapsed < HOUR) {
    return `${Math.round(elapsed / MINUTE)} minutes ago`;
  }
  if (elapsed < 2 * HOUR) {
    return '1 hour ago';
  }
  if (elapsed < DAY) {
    return `${Math.round(elapsed / HOUR)} hours ago`;
  }
  if (elapsed < 2 * DAY) {
    return 'yesterday';
  }
  if (elapsed < WEEK) {
    return `${Math.round(elapsed / DAY)} days ago`;
  }
  if (elapsed < 5 * WEEK) {
    return `${Math.round(elapsed / WEEK)} weeks ago`;
  }
  if (elapsed < 365 * DAY) {
    return `${Math.round(elapsed / (30 * DAY))} months ago`;
  }

  const years = Math.round(elapsed / (365 * DAY));
  return years <= 1 ? '1 year ago' : `${years} years ago`;
}

import { formatRelativeTime } from '../time';

/** A fixed "now" so the expectations never depend on the wall clock. */
const now = new Date('2026-10-09T12:00:00.000Z');

/** Returns the timestamp `milliseconds` before `now`, as the API would send it. */
function ago(milliseconds: number): string {
  return new Date(now.getTime() - milliseconds).toISOString();
}

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;

describe('formatRelativeTime', () => {
  it('reads a very recent timestamp as "just now"', () => {
    expect(formatRelativeTime(ago(0), now)).toBe('just now');
    expect(formatRelativeTime(ago(30 * SECOND), now)).toBe('just now');
  });

  it('reads a future timestamp as "just now" rather than a negative age', () => {
    const future = new Date(now.getTime() + 5 * MINUTE).toISOString();

    expect(formatRelativeTime(future, now)).toBe('just now');
  });

  it('uses singular wording for one minute and one hour', () => {
    expect(formatRelativeTime(ago(MINUTE), now)).toBe('1 minute ago');
    expect(formatRelativeTime(ago(HOUR + MINUTE), now)).toBe('1 hour ago');
  });

  it('counts minutes, hours and days', () => {
    expect(formatRelativeTime(ago(5 * MINUTE), now)).toBe('5 minutes ago');
    expect(formatRelativeTime(ago(3 * HOUR), now)).toBe('3 hours ago');
    expect(formatRelativeTime(ago(3 * DAY), now)).toBe('3 days ago');
  });

  it('reads the previous day as "yesterday"', () => {
    expect(formatRelativeTime(ago(DAY + HOUR), now)).toBe('yesterday');
  });

  it('counts weeks, months and years', () => {
    expect(formatRelativeTime(ago(2 * WEEK), now)).toBe('2 weeks ago');
    expect(formatRelativeTime(ago(90 * DAY), now)).toBe('3 months ago');
    expect(formatRelativeTime(ago(400 * DAY), now)).toBe('1 year ago');
    expect(formatRelativeTime(ago(3 * 365 * DAY), now)).toBe('3 years ago');
  });

  it('shows an unparseable timestamp verbatim', () => {
    expect(formatRelativeTime('not a timestamp', now)).toBe('not a timestamp');
    expect(formatRelativeTime('', now)).toBe('');
  });

  it('compares across time zones by instant, not by field value', () => {
    // The same instant, written in a +02:00 offset rather than as UTC.
    const shifted = '2026-10-09T11:00:00.000+02:00';

    expect(formatRelativeTime(shifted, now)).toBe('3 hours ago');
  });
});

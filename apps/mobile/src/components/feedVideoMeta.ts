/**
 * Pure presentation helpers for the feed's auto-playing video (KNOT-020c-fix3).
 *
 * The formatting lives apart from the component so it can be covered by the
 * project's renderer-less test pattern: `FeedVideo` holds the hooks, this module
 * holds the arithmetic.
 *
 * The file is named `feedVideoMeta.ts`, not `feedVideo.ts`, because macOS resolves
 * paths case-insensitively: a module differing from `FeedVideo.tsx` only in case
 * makes TypeScript resolve both to one file (KNOT-020c-fix3).
 */

/** Zero-pads a value below ten, so a badge reads "1:04" rather than "1:4". */
function pad(value: number): string {
  return value < 10 ? `0${value}` : String(value);
}

/**
 * Formats a video's length for the duration badge: "m:ss", or "h:mm:ss" once it
 * runs past an hour.
 *
 * The empty string means "no duration to show" — the caller renders no badge —
 * which covers a video whose metadata has not loaded yet (`0`) as well as a
 * nonsensical value (negative, `NaN`, `Infinity`). Rounding means a 64.4-second
 * clip reads as "1:04".
 */
export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return '';
  }

  const total = Math.round(seconds);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const secs = total % 60;

  if (hours > 0) {
    return `${hours}:${pad(minutes)}:${pad(secs)}`;
  }

  return `${minutes}:${pad(secs)}`;
}

/**
 * Formats the playback position against the clip's length: "0:15 / 1:16".
 *
 * The badge shows where the video has reached as well as how long it runs, so a
 * preview that autoplays as it scrolls into view reads as playing rather than as
 * a still with a length printed on it. The arithmetic is `formatDuration`'s, so
 * both halves round the same way and the pair never disagrees by a second.
 *
 * The empty string means the clip's length is not known yet (or is nonsensical),
 * so there is nothing to count against and no badge is drawn. A position of zero
 * draws as "0:00" — `formatDuration` returns the empty string for it, because for
 * a length that is the honest answer, while for a position the badge still needs
 * a starting point.
 */
export function formatProgress(currentSeconds: number, durationSeconds: number): string {
  const duration = formatDuration(durationSeconds);
  if (duration === '') {
    return '';
  }

  const position = currentSeconds > 0 ? formatDuration(currentSeconds) : '';
  return `${position === '' ? '0:00' : position} / ${duration}`;
}

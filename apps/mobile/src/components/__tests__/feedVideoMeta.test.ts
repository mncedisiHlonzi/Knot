import { formatDuration, formatProgress } from '../feedVideoMeta';

describe('formatDuration', () => {
  it('formats a clip under an hour as m:ss', () => {
    expect(formatDuration(64)).toBe('1:04');
    expect(formatDuration(59)).toBe('0:59');
    expect(formatDuration(600)).toBe('10:00');
  });

  it('zero-pads the seconds', () => {
    expect(formatDuration(5)).toBe('0:05');
    expect(formatDuration(65)).toBe('1:05');
  });

  it('formats an hour or more as h:mm:ss', () => {
    expect(formatDuration(3600)).toBe('1:00:00');
    expect(formatDuration(3725)).toBe('1:02:05');
    expect(formatDuration(36000)).toBe('10:00:00');
  });

  it('rounds to the nearest second', () => {
    expect(formatDuration(64.4)).toBe('1:04');
    expect(formatDuration(64.6)).toBe('1:05');
  });

  it('returns an empty string when there is no duration to show', () => {
    expect(formatDuration(0)).toBe('');
    expect(formatDuration(-1)).toBe('');
    expect(formatDuration(Number.NaN)).toBe('');
    expect(formatDuration(Number.POSITIVE_INFINITY)).toBe('');
  });
});

describe('formatProgress', () => {
  it('reads position over length', () => {
    expect(formatProgress(15, 76)).toBe('0:15 / 1:16');
    expect(formatProgress(5, 64)).toBe('0:05 / 1:04');
  });

  it('starts a known clip at zero', () => {
    expect(formatProgress(0, 76)).toBe('0:00 / 1:16');
  });

  it('formats the position with the same rounding as the length', () => {
    expect(formatProgress(15.4, 76.4)).toBe('0:15 / 1:16');
    expect(formatProgress(15.6, 76.6)).toBe('0:16 / 1:17');
  });

  it('shows nothing until the length is known', () => {
    expect(formatProgress(15, 0)).toBe('');
    expect(formatProgress(0, Number.NaN)).toBe('');
    expect(formatProgress(15, Number.POSITIVE_INFINITY)).toBe('');
  });

  it('never prints a broken position', () => {
    expect(formatProgress(Number.NaN, 76)).toBe('0:00 / 1:16');
    expect(formatProgress(-3, 76)).toBe('0:00 / 1:16');
  });
});

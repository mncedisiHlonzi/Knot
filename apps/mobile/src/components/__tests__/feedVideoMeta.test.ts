import { formatDuration } from '../feedVideoMeta';

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

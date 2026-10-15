import {
  FALLBACK_MEDIA_ASPECT_RATIO,
  MAX_PORTRAIT_ASPECT_RATIO,
  PLACEHOLDER_MEDIA_ASPECT_RATIO,
  hasMediaDimensions,
  mediaAspectRatio,
  resolveMediaAspectRatio,
} from '../mediaFrame';

describe('mediaAspectRatio', () => {
  it("uses the media's own ratio when both dimensions are known", () => {
    expect(mediaAspectRatio(1920, 1080)).toBeCloseTo(16 / 9, 4);
    expect(mediaAspectRatio(1000, 1000)).toBe(1);
    // A 4:5 photo is exactly the cap, so it passes through untouched.
    expect(mediaAspectRatio(1000, 1250)).toBeCloseTo(0.8, 6);
  });

  it('caps a taller portrait ratio at 4:5, so no frame exceeds the maximum height', () => {
    // 9:16 (0.5625) is far taller than the cap, so it is lifted to 0.8 — the frame
    // is at most `width x 1.25` (KNOT-020g-fix12).
    expect(mediaAspectRatio(1080, 1920)).toBe(MAX_PORTRAIT_ASPECT_RATIO);
    expect(mediaAspectRatio(300, 700)).toBe(MAX_PORTRAIT_ASPECT_RATIO);
    expect(mediaAspectRatio(1080, 1920)).toBeCloseTo(4 / 5, 6);
  });

  it('leaves landscape media untouched, because every landscape ratio clears the cap', () => {
    expect(mediaAspectRatio(1920, 1080)).toBeCloseTo(1920 / 1080, 6);
    expect(mediaAspectRatio(1600, 900)).toBeCloseTo(1600 / 900, 6);
    // Just wider than the cap: still untouched.
    expect(mediaAspectRatio(900, 1000)).toBeCloseTo(0.9, 6);
  });

  it('falls back when neither dimension is known', () => {
    expect(mediaAspectRatio()).toBe(FALLBACK_MEDIA_ASPECT_RATIO);
    expect(mediaAspectRatio(null, null)).toBe(FALLBACK_MEDIA_ASPECT_RATIO);
    expect(mediaAspectRatio(undefined, undefined)).toBe(FALLBACK_MEDIA_ASPECT_RATIO);
  });

  it('falls back when only one of the pair is known, since half a pair is no shape', () => {
    expect(mediaAspectRatio(1080, null)).toBe(FALLBACK_MEDIA_ASPECT_RATIO);
    expect(mediaAspectRatio(null, 1920)).toBe(FALLBACK_MEDIA_ASPECT_RATIO);
    expect(mediaAspectRatio(1080, undefined)).toBe(FALLBACK_MEDIA_ASPECT_RATIO);
  });

  it('falls back for a zero or negative dimension rather than dividing by it', () => {
    expect(mediaAspectRatio(0, 1920)).toBe(FALLBACK_MEDIA_ASPECT_RATIO);
    expect(mediaAspectRatio(1080, 0)).toBe(FALLBACK_MEDIA_ASPECT_RATIO);
    expect(mediaAspectRatio(-1080, 1920)).toBe(FALLBACK_MEDIA_ASPECT_RATIO);
    expect(mediaAspectRatio(1080, -1920)).toBe(FALLBACK_MEDIA_ASPECT_RATIO);
  });

  it('treats a legacy payload with no dimension fields as unknown', () => {
    const payload: { first_media_width?: number; first_media_height?: number } = {};
    expect(mediaAspectRatio(payload.first_media_width, payload.first_media_height)).toBe(
      FALLBACK_MEDIA_ASPECT_RATIO,
    );
  });

  it('reports the fallback as 16:9, the ratio the frame used before this rule existed', () => {
    expect(FALLBACK_MEDIA_ASPECT_RATIO).toBeCloseTo(16 / 9, 6);
  });
});

describe('hasMediaDimensions', () => {
  it('is true only for a complete, positive pair', () => {
    expect(hasMediaDimensions(1080, 1920)).toBe(true);
    expect(hasMediaDimensions(1080, null)).toBe(false);
    expect(hasMediaDimensions(0, 1920)).toBe(false);
    expect(hasMediaDimensions()).toBe(false);
  });
});

describe('MAX_PORTRAIT_ASPECT_RATIO', () => {
  it('is 4:5, the tallest shape a list frame takes', () => {
    expect(MAX_PORTRAIT_ASPECT_RATIO).toBeCloseTo(4 / 5, 6);
  });

  it('caps a frame at 1.25x its width', () => {
    const width = 400;
    expect(width / MAX_PORTRAIT_ASPECT_RATIO).toBeCloseTo(width * 1.25, 6);
  });

  it('is narrower than the square placeholder and the landscape fallback', () => {
    expect(MAX_PORTRAIT_ASPECT_RATIO).toBeLessThan(PLACEHOLDER_MEDIA_ASPECT_RATIO);
    expect(MAX_PORTRAIT_ASPECT_RATIO).toBeLessThan(FALLBACK_MEDIA_ASPECT_RATIO);
  });
});

describe('resolveMediaAspectRatio', () => {
  it('prefers the payload, which is known before the media is fetched', () => {
    expect(resolveMediaAspectRatio(1080, 1920, 1, 1)).toBe(MAX_PORTRAIT_ASPECT_RATIO);
    expect(resolveMediaAspectRatio(1920, 1080, null, null)).toBeCloseTo(16 / 9, 4);
  });

  it('falls to the loaded size when the payload cannot describe the media', () => {
    expect(resolveMediaAspectRatio(undefined, undefined, 300, 700)).toBe(MAX_PORTRAIT_ASPECT_RATIO);
    expect(resolveMediaAspectRatio(null, null, 1920, 1080)).toBeCloseTo(16 / 9, 4);
  });

  it('applies the cap to the loaded size too, so a tall video cannot run off the page', () => {
    expect(resolveMediaAspectRatio(undefined, undefined, 1080, 1920)).toBe(
      MAX_PORTRAIT_ASPECT_RATIO,
    );
    expect(resolveMediaAspectRatio(null, null, 1000, 1250)).toBeCloseTo(0.8, 6);
  });

  it('ignores a half pair on either side, since half a pair is no shape', () => {
    expect(resolveMediaAspectRatio(1080, null, 300, 700)).toBe(MAX_PORTRAIT_ASPECT_RATIO);
    expect(resolveMediaAspectRatio(null, 1920, 300, 700)).toBe(MAX_PORTRAIT_ASPECT_RATIO);
    expect(resolveMediaAspectRatio(undefined, undefined, 300, null)).toBeNull();
  });

  it('reports unknown rather than guessing a fixed ratio', () => {
    expect(resolveMediaAspectRatio()).toBeNull();
    expect(resolveMediaAspectRatio(null, null)).toBeNull();
    expect(resolveMediaAspectRatio(undefined, undefined, null, null)).toBeNull();
    // A zero dimension is a client that reported nothing, not a shape.
    expect(resolveMediaAspectRatio(0, 0, 0, 0)).toBeNull();
  });

  it('never returns the fixed fallback, which would reinstate the crop', () => {
    const resolved = [
      resolveMediaAspectRatio(300, 700),
      resolveMediaAspectRatio(undefined, undefined, 300, 700),
      resolveMediaAspectRatio(),
    ];

    expect(resolved).not.toContain(FALLBACK_MEDIA_ASPECT_RATIO);
    expect(resolved[2]).toBeNull();
  });
});

describe('PLACEHOLDER_MEDIA_ASPECT_RATIO', () => {
  it('is a square, so the pre-load frame promises no shape', () => {
    expect(PLACEHOLDER_MEDIA_ASPECT_RATIO).toBe(1);
  });
});

import {
  ONBOARDING_SLIDE_COUNT,
  ONBOARDING_SLIDES,
  advance,
  continueLabel,
  isLastSlide,
} from '../onboarding';

describe('onboarding copy', () => {
  it('has exactly three slides', () => {
    expect(ONBOARDING_SLIDE_COUNT).toBe(3);
    expect(ONBOARDING_SLIDES).toHaveLength(3);
  });

  it('carries a headline, a body, and an illustration for every slide', () => {
    for (const slide of ONBOARDING_SLIDES) {
      expect(slide.headline.trim()).not.toBe('');
      expect(slide.body.trim()).not.toBe('');
      expect(slide.illustration).not.toBe('');
    }
  });
});

describe('onboarding state machine', () => {
  it('labels the primary action "Continue" until the last slide', () => {
    expect(continueLabel(0)).toBe('Continue');
    expect(continueLabel(1)).toBe('Continue');
  });

  it('labels the primary action "Get started" on the last slide', () => {
    expect(continueLabel(2)).toBe('Get started');
    // An out-of-range index is treated as the last slide.
    expect(continueLabel(99)).toBe('Get started');
  });

  it('advances through the slides and finishes on the last one', () => {
    expect(advance(0)).toEqual({ done: false, index: 1 });
    expect(advance(1)).toEqual({ done: false, index: 2 });
    expect(advance(2)).toEqual({ done: true, index: 2 });
  });

  it('reports the last slide for the final and any out-of-range index', () => {
    expect(isLastSlide(0)).toBe(false);
    expect(isLastSlide(1)).toBe(false);
    expect(isLastSlide(2)).toBe(true);
    expect(isLastSlide(7)).toBe(true);
  });
});

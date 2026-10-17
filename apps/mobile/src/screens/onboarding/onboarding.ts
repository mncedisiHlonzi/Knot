/**
 * Onboarding copy and state machine (KNOT-020c).
 *
 * The three first-run slides and the pure arithmetic of advancing them live here,
 * apart from the screen, so the "Continue"/"Get started" behaviour can be tested
 * without a renderer. The copy is fixed product text, held in the order it is
 * shown.
 */

/** Which illustration a slide draws. Two are placeholders pending real artwork. */
export type OnboardingIllustration = 'hero' | 'placeholder-a' | 'placeholder-b';

/** One onboarding slide: its artwork key, headline, and body. */
export type OnboardingSlide = {
  readonly illustration: OnboardingIllustration;
  readonly headline: string;
  readonly body: string;
};

/** The three slides, in order. Copy is verbatim product text (KNOT-020c). */
export const ONBOARDING_SLIDES: readonly OnboardingSlide[] = [
  {
    illustration: 'hero',
    headline: 'Real people. Real stories. Every language.',
    body: 'Knot is a place where people share stories from their own lives and communities — and adapt them for people in other languages.',
  },
  {
    illustration: 'placeholder-a',
    headline: 'Adapt for your people.',
    body: 'When a story reaches you in another language, you can tell it again for your community — preserving context, meaning, and voice.',
  },
  {
    illustration: 'placeholder-b',
    headline: 'Understand across the divide.',
    body: 'Conversations happen in every language. Understanding travels both ways.',
  },
];

/** How many slides there are. */
export const ONBOARDING_SLIDE_COUNT = ONBOARDING_SLIDES.length;

/** Whether `index` is the final slide. An out-of-range index reads as the last. */
export function isLastSlide(index: number): boolean {
  return index >= ONBOARDING_SLIDE_COUNT - 1;
}

/**
 * The primary button's label on a given slide: "Get started" on the last slide,
 * "Continue" before it.
 */
export function continueLabel(index: number): 'Continue' | 'Get started' {
  return isLastSlide(index) ? 'Get started' : 'Continue';
}

/** The result of pressing the primary button on a slide. */
export type OnboardingAdvance = {
  /** The slide to show next. Unchanged when `done`. */
  readonly index: number;
  /** True when the flow is finished and the caller should leave onboarding. */
  readonly done: boolean;
};

/**
 * Advances the onboarding flow one step.
 *
 * Before the last slide this moves to the next index; on the last slide it does
 * not move and reports `done`, so the caller navigates on to the auth screens.
 * An already-out-of-range index is treated as the last slide.
 */
export function advance(index: number): OnboardingAdvance {
  if (isLastSlide(index)) {
    return { done: true, index };
  }
  return { done: false, index: index + 1 };
}

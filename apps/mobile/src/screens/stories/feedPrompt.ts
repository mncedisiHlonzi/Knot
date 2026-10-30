/**
 * The Feed prompt card's content, and the rules the card uses to move between
 * prompts (KNOT-020k-fix7).
 *
 * The card asks one question at a time, so that telling a story is the obvious
 * next thing to do rather than a tab you have to remember. There is no prompt
 * endpoint — the list is compiled into the app for now — so the content and the
 * selection rules live here, apart from the screen, and are covered without a
 * renderer (the `profileActivity.ts` / `feedEntryMeta.ts` pattern).
 *
 * Pillars are the app's own two story families, so a prompt never invents a
 * vocabulary the rest of the product does not speak.
 */
import type { Pillar } from '../../components/PillarTag';

/** One writing prompt: a question, and the pillar it belongs to. */
export type FeedPrompt = {
  /** Stable id, so the card can re-key its text when the prompt changes. */
  readonly id: string;
  /** The family the prompt belongs to; the card's overline and chips read it. */
  readonly pillar: Pillar;
  /** The question the card asks. */
  readonly text: string;
};

/**
 * The prompts the card cycles through.
 *
 * Written in the product's voice: a question a person can answer from their own
 * life, in a sentence or two — not a topic to research. Three per pillar, so
 * choosing a pillar always has somewhere to go.
 */
export const PROMPTS: readonly FeedPrompt[] = [
  {
    id: 'wonder-older-teacher',
    pillar: 'wonder',
    text: 'What did someone older teach you that school never did?',
  },
  {
    id: 'heritage-place-speech',
    pillar: 'heritage',
    text: 'Which place shaped the way you speak?',
  },
  {
    id: 'wonder-small-surprise',
    pillar: 'wonder',
    text: 'What small thing still surprises you every time?',
  },
  {
    id: 'heritage-family-word',
    pillar: 'heritage',
    text: 'What is a word only your family uses?',
  },
  {
    id: 'wonder-advice-at-sixteen',
    pillar: 'wonder',
    text: 'What do you know now that you wish you had at sixteen?',
  },
  {
    id: 'heritage-carried-recipe',
    pillar: 'heritage',
    text: 'Which recipe would you carry to another country?',
  },
];

/** The pillars, in the order the card's chips draw them. */
export const PROMPT_PILLARS: readonly Pillar[] = ['wonder', 'heritage'];

/**
 * An index wrapped into the list's range.
 *
 * The card keeps one number as its state, so every read goes through this: a
 * stale index (after the list shrinks in a later release) can never point past
 * the end of the array.
 */
function normalise(index: number): number {
  const count = PROMPTS.length;
  const whole = Number.isFinite(index) ? Math.trunc(index) : 0;

  return ((whole % count) + count) % count;
}

/** The prompt the card shows for an index, wrapped into range. */
export function promptAt(index: number): FeedPrompt {
  return PROMPTS[normalise(index)];
}

/** The pillar of the prompt at an index. */
export function promptPillarOf(index: number): Pillar {
  return promptAt(index).pillar;
}

/**
 * The next prompt: a shuffle that never lands on the one already on screen.
 *
 * A rotation rather than a random pick, so the same tap always produces the same
 * card and the behaviour is testable. With a handful of prompts, "next" reads as
 * a shuffle anyway.
 */
export function nextPromptIndex(index: number): number {
  return (normalise(index) + 1) % PROMPTS.length;
}

/**
 * The next prompt belonging to `pillar`, searching forward from `index`.
 *
 * Searching from the step AFTER the current prompt is what makes tapping the
 * chip that is already selected useful: it deals the next prompt of the same
 * family instead of doing nothing. When no prompt of that family exists (only
 * possible if the list is edited to drop one), the current index is kept.
 */
export function promptIndexForPillar(index: number, pillar: Pillar): number {
  const current = normalise(index);

  for (let step = 1; step <= PROMPTS.length; step += 1) {
    const candidate = (current + step) % PROMPTS.length;
    if (PROMPTS[candidate].pillar === pillar) {
      return candidate;
    }
  }

  return current;
}

/**
 * The word the card's overline shows for a pillar.
 *
 * It says "story", not "prompt": the prompt is the question, and the family is the
 * kind of story it is asking for (KNOT-020k-fix8). It is written in sentence case
 * because `typeScale.overline` carries the uppercasing and the letter-spacing, so
 * the copy stays readable in the source.
 */
export function promptPillarLabel(pillar: Pillar): string {
  return pillar === 'wonder' ? 'Wonder story' : 'Heritage story';
}

/** The word a pillar chip shows. */
export function promptPillarName(pillar: Pillar): string {
  return pillar === 'wonder' ? 'Wonder' : 'Heritage';
}

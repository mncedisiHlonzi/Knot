import {
  PROMPTS,
  PROMPT_PILLARS,
  nextPromptIndex,
  promptAt,
  promptIndexForPillar,
  promptPillarLabel,
  promptPillarName,
  promptPillarOf,
} from '../feedPrompt';

/**
 * The Feed prompt card's content and selection rules are pure, so they are covered
 * directly: no renderer, no React Native, no network (KNOT-020k-fix7).
 */
describe('feedPrompt', () => {
  it('offers a prompt for every pillar, more than one of each', () => {
    expect(PROMPTS.length).toBeGreaterThanOrEqual(4);

    for (const pillar of PROMPT_PILLARS) {
      const count = PROMPTS.filter((prompt) => prompt.pillar === pillar).length;
      expect(count).toBeGreaterThan(1);
    }
  });

  it('gives every prompt a stable id and a question', () => {
    const ids = PROMPTS.map((prompt) => prompt.id);

    expect(new Set(ids).size).toBe(ids.length);
    for (const prompt of PROMPTS) {
      expect(prompt.text.endsWith('?')).toBe(true);
    }
  });

  // The card holds a single index, so every read has to be safe: a negative, a
  // fractional or an out-of-range number still names a prompt.
  it('wraps any index into the list', () => {
    expect(promptAt(0)).toBe(PROMPTS[0]);
    expect(promptAt(PROMPTS.length)).toBe(PROMPTS[0]);
    expect(promptAt(-1)).toBe(PROMPTS[PROMPTS.length - 1]);
    expect(promptAt(1.7)).toBe(PROMPTS[1]);
    expect(promptAt(Number.NaN)).toBe(PROMPTS[0]);
  });

  it('reads the pillar of the prompt on screen', () => {
    expect(promptPillarOf(0)).toBe(PROMPTS[0].pillar);
    expect(promptPillarOf(PROMPTS.length + 1)).toBe(PROMPTS[1].pillar);
  });

  // A shuffle that can land on the prompt already showing would look broken.
  it('always moves to a different prompt', () => {
    for (let index = 0; index < PROMPTS.length; index += 1) {
      const next = nextPromptIndex(index);

      expect(next).not.toBe(index);
      expect(promptAt(next).id).not.toBe(promptAt(index).id);
    }

    // It is a rotation, so a full pass returns to where it started.
    let index = 0;
    for (let step = 0; step < PROMPTS.length; step += 1) {
      index = nextPromptIndex(index);
    }
    expect(index).toBe(0);
  });

  // Tapping a chip deals the next prompt of that family. Searching from the step
  // after the current prompt is what makes tapping the selected chip useful.
  it('finds the next prompt of a chosen pillar', () => {
    for (const pillar of PROMPT_PILLARS) {
      const next = promptIndexForPillar(0, pillar);
      expect(promptAt(next).pillar).toBe(pillar);

      // And again, from the prompt it just landed on: it keeps moving within the
      // family rather than sticking.
      const again = promptIndexForPillar(next, pillar);
      expect(promptAt(again).pillar).toBe(pillar);
      expect(again).not.toBe(next);
    }
  });

  it('names the pillar for the overline and the chips', () => {
    // Sentence case: `typeScale.overline` supplies the uppercasing.
    expect(promptPillarLabel('wonder')).toBe('Wonder prompt');
    expect(promptPillarLabel('heritage')).toBe('Heritage prompt');
    expect(promptPillarName('wonder')).toBe('Wonder');
    expect(promptPillarName('heritage')).toBe('Heritage');
  });
});

import type { RecentInteraction } from '../../api/stories';
import {
  attributionRuns,
  hasSocialProof,
  SOCIAL_PROOF_MAX_ACTORS,
  socialProofActors,
  socialProofRuns,
} from '../socialProof';

/** One interaction fixture; overrides name the field under test. */
function interaction(overrides: Partial<RecentInteraction>): RecentInteraction {
  return {
    type: 'reaction',
    actor_display_name: 'Tycoon',
    actor_avatar_url: null,
    detail: 'rings_true',
    ...overrides,
  };
}

/** The sentence the runs draw, with every run joined. */
function sentence(interactions: readonly RecentInteraction[]): string {
  return socialProofRuns(interactions)
    .map((run) => run.text)
    .join('');
}

/** The names the sentence bolds. */
function boldNames(interactions: readonly RecentInteraction[]): string[] {
  return socialProofRuns(interactions)
    .filter((run) => run.bold)
    .map((run) => run.text);
}

describe('socialProofRuns', () => {
  it('draws nothing when there are no interactions', () => {
    expect(socialProofRuns([])).toEqual([]);
  });

  it('formats one reaction: name, signal, "this"', () => {
    const runs = socialProofRuns([interaction({ detail: 'needs_a_source' })]);

    expect(runs.map((run) => run.text).join('')).toBe('Tycoon needs a source this');
    expect(boldNames([interaction({ detail: 'needs_a_source' })])).toEqual(['Tycoon']);
  });

  it('formats one comment', () => {
    expect(
      sentence([interaction({ type: 'comment', detail: null, actor_display_name: 'Yantuntu' })]),
    ).toBe('Yantuntu commented on this');
  });

  it('groups two reactions of the same signal', () => {
    expect(
      sentence([
        interaction({ actor_display_name: 'Tycoon', detail: 'know_it_differently' }),
        interaction({ actor_display_name: 'Yantuntu', detail: 'know_it_differently' }),
      ]),
    ).toBe('Tycoon & Yantuntu know it differently this');
  });

  it('groups two comments', () => {
    expect(
      sentence([
        interaction({ type: 'comment', detail: null, actor_display_name: 'Tycoon' }),
        interaction({ type: 'comment', detail: null, actor_display_name: 'Yantuntu' }),
      ]),
    ).toBe('Tycoon & Yantuntu commented on this');
  });

  it('joins mixed actions with "and" and a shared "this"', () => {
    expect(
      sentence([
        interaction({ actor_display_name: 'Tycoon', detail: 'rings_true' }),
        interaction({ type: 'comment', detail: null, actor_display_name: 'Yantuntu' }),
      ]),
    ).toBe('Tycoon rings true and Yantuntu commented on this');
  });

  it('keeps two different reaction signals as two clauses', () => {
    expect(
      sentence([
        interaction({ actor_display_name: 'Tycoon', detail: 'rings_true' }),
        interaction({ actor_display_name: 'Yantuntu', detail: 'needs_a_source' }),
      ]),
    ).toBe('Tycoon rings true and Yantuntu needs a source this');
  });

  it('handles a comment before a reaction', () => {
    expect(
      sentence([
        interaction({ type: 'comment', detail: null, actor_display_name: 'Tycoon' }),
        interaction({ actor_display_name: 'Yantuntu', detail: 'rings_true' }),
      ]),
    ).toBe('Tycoon commented on and Yantuntu rings true this');
  });

  it('draws at most two actors', () => {
    const many = [
      interaction({ actor_display_name: 'First' }),
      interaction({ actor_display_name: 'Second' }),
      interaction({ actor_display_name: 'Third' }),
    ];

    expect(SOCIAL_PROOF_MAX_ACTORS).toBe(2);
    expect(socialProofActors(many)).toHaveLength(2);
    expect(boldNames(many)).toEqual(['First', 'Second']);
  });

  it('degrades an unknown reaction type to a generic verb', () => {
    expect(sentence([interaction({ detail: 'shares_something' })])).toBe('Tycoon reacted to this');
  });

  it('names a blank actor "Someone" so the sentence stays grammatical', () => {
    expect(sentence([interaction({ actor_display_name: '  ' })])).toBe('Someone rings true this');
  });
});

describe('attributionRuns', () => {
  it('splits the server sentence and bolds its names', () => {
    const runs = attributionRuns(
      "Adapted by Nomsa Khan from Ada Lovelace's original story in English.",
      'Nomsa Khan',
      'Ada Lovelace',
    );

    expect(runs.filter((run) => run.bold).map((run) => run.text)).toEqual([
      'Nomsa Khan',
      'Ada Lovelace',
    ]);
  });

  it('builds the fallback sentence from the structured lineage', () => {
    expect(
      attributionRuns('', 'Nomsa Khan', 'Ada Lovelace')
        .map((run) => run.text)
        .join(''),
    ).toBe("Adapted by Nomsa Khan from Ada Lovelace's original story.");
  });

  it('omits the adapter when their name is unknown', () => {
    expect(
      attributionRuns('', '', 'Ada Lovelace')
        .map((run) => run.text)
        .join(''),
    ).toBe("Adapted from Ada Lovelace's original story.");
  });

  it('draws nothing when there is no sentence and no root author', () => {
    expect(attributionRuns('', 'Nomsa Khan', '')).toEqual([]);
    expect(attributionRuns(undefined, '', '')).toEqual([]);
  });
});

describe('hasSocialProof', () => {
  it('is true for interactions, a sentence, or a known root author', () => {
    expect(hasSocialProof([interaction({})], '', '')).toBe(true);
    expect(hasSocialProof([], 'Original story by Ada Lovelace.', '')).toBe(true);
    expect(hasSocialProof([], '', 'Ada Lovelace')).toBe(true);
  });

  it('is false when there is nothing to credit', () => {
    expect(hasSocialProof([], '', '')).toBe(false);
    expect(hasSocialProof(undefined, '   ', '')).toBe(false);
  });
});

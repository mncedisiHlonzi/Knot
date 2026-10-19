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

  it('formats one reaction as a sentence, with no trailing "this"', () => {
    const runs = socialProofRuns([interaction({ detail: 'needs_a_source' })]);

    expect(runs.map((run) => run.text).join('')).toBe('Tycoon needs a source.');
    expect(boldNames([interaction({ detail: 'needs_a_source' })])).toEqual(['Tycoon']);
  });

  it('uses a natural verb for every reaction', () => {
    const cases = [
      ['rings_true', 'Tycoon finds this true.'],
      ['know_it_differently', 'Tycoon knows this differently.'],
      ['adds_something_new', 'Tycoon adds something new.'],
      ['needs_a_source', 'Tycoon needs a source.'],
    ] as const;

    for (const [detail, expected] of cases) {
      expect(sentence([interaction({ detail })])).toBe(expected);
    }
  });

  it('formats one comment', () => {
    expect(
      sentence([interaction({ type: 'comment', detail: null, actor_display_name: 'Yantuntu' })]),
    ).toBe('Yantuntu commented on this.');
  });

  it('names one actor once when they both reacted and commented', () => {
    const both = [
      interaction({ actor_display_name: 'Yantuntu', detail: 'rings_true' }),
      interaction({ type: 'comment', detail: null, actor_display_name: 'Yantuntu' }),
    ];

    expect(sentence(both)).toBe('Yantuntu finds this true and commented.');
    expect(boldNames(both)).toEqual(['Yantuntu']);
    // One actor, not two: the avatar stack draws one face and the name once.
    expect(socialProofActors(both)).toHaveLength(1);
  });

  it('puts the reaction first even when the comment is newer', () => {
    const both = [
      interaction({ type: 'comment', detail: null, actor_display_name: 'Yantuntu' }),
      interaction({ actor_display_name: 'Yantuntu', detail: 'know_it_differently' }),
    ];

    expect(sentence(both)).toBe('Yantuntu knows this differently and commented.');
    expect(boldNames(both)).toEqual(['Yantuntu']);
  });

  it('groups two reactions of the same signal', () => {
    expect(
      sentence([
        interaction({ actor_display_name: 'Tycoon', detail: 'know_it_differently' }),
        interaction({ actor_display_name: 'Yantuntu', detail: 'know_it_differently' }),
      ]),
    ).toBe('Tycoon & Yantuntu know this differently.');
  });

  it('groups two comments', () => {
    expect(
      sentence([
        interaction({ type: 'comment', detail: null, actor_display_name: 'Tycoon' }),
        interaction({ type: 'comment', detail: null, actor_display_name: 'Yantuntu' }),
      ]),
    ).toBe('Tycoon & Yantuntu commented on this.');
  });

  it('joins mixed actions with "and" and a shared "this"', () => {
    expect(
      sentence([
        interaction({ actor_display_name: 'Tycoon', detail: 'rings_true' }),
        interaction({ type: 'comment', detail: null, actor_display_name: 'Yantuntu' }),
      ]),
    ).toBe('Tycoon finds this true and Yantuntu commented on this.');
  });

  it('keeps two different reaction signals as two clauses', () => {
    expect(
      sentence([
        interaction({ actor_display_name: 'Tycoon', detail: 'rings_true' }),
        interaction({ actor_display_name: 'Yantuntu', detail: 'needs_a_source' }),
      ]),
    ).toBe('Tycoon finds this true and Yantuntu needs a source.');
  });

  it('handles a comment before a reaction', () => {
    expect(
      sentence([
        interaction({ type: 'comment', detail: null, actor_display_name: 'Tycoon' }),
        interaction({ actor_display_name: 'Yantuntu', detail: 'rings_true' }),
      ]),
    ).toBe('Tycoon commented on this and Yantuntu finds this true.');
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
    expect(sentence([interaction({ detail: 'shares_something' })])).toBe('Tycoon reacted to this.');
  });

  it('names a blank actor "Someone" so the sentence stays grammatical', () => {
    expect(sentence([interaction({ actor_display_name: '  ' })])).toBe('Someone finds this true.');
  });
});

describe('attributionRuns', () => {
  it('splits the server sentence and bolds its names', () => {
    const runs = attributionRuns(
      "Adapted from Ada Lovelace's original story in English.",
      'Ada Lovelace',
    );

    expect(runs.filter((run) => run.bold).map((run) => run.text)).toEqual(['Ada Lovelace']);
  });

  it('builds the concise fallback sentence from the structured lineage', () => {
    expect(
      attributionRuns('', 'Ada Lovelace')
        .map((run) => run.text)
        .join(''),
    ).toBe("Adapted from Ada Lovelace's original story.");
  });

  it('draws no attribution when there is no sentence and no root author', () => {
    expect(attributionRuns('', '')).toEqual([]);
    expect(attributionRuns(undefined, '')).toEqual([]);
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

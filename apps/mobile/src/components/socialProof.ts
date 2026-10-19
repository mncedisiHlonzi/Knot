/**
 * The display rules for a feed entry's social-proof header (KNOT-028).
 *
 * The header answers one question: who has done something to this entry lately?
 * It shows at most two actors. When there are interactions it names them and
 * their action; when there are none it falls back to the server's hereditary
 * attribution sentence (or the structured lineage that stands in for it). When
 * neither exists there is nothing to draw.
 *
 * Everything here is pure so the wording can be tested without a renderer. The
 * component in `SocialProofHeader.tsx` only chooses which set of runs to draw.
 */
import type { RecentInteraction } from '../api/stories';
import { attributionSegments, type AttributionSegment } from './feedEntryMeta';

/** The most actors the header will name. */
export const SOCIAL_PROOF_MAX_ACTORS = 2;

/** One run of the header's sentence; `bold` marks a person's name. */
export type SocialProofRun = {
  readonly text: string;
  readonly bold: boolean;
};

/**
 * How one reaction reads inside the sentence, in both grammatical numbers.
 *
 * The header is a sentence, not a label list, so the signal is a verb phrase and
 * it carries the object itself ("finds this true", "knows this differently") —
 * there is no trailing "this" bolted onto the end. Two actors sharing a signal
 * take the plural form ("know this differently"); an unknown future signal
 * degrades to the generic phrase rather than rendering an empty gap (KNOT-029).
 */
type ReactionPhrase = {
  readonly singular: string;
  readonly plural: string;
};

const REACTION_PHRASES: Readonly<Record<string, ReactionPhrase>> = {
  rings_true: { singular: 'finds this true', plural: 'find this true' },
  know_it_differently: { singular: 'knows this differently', plural: 'know this differently' },
  adds_something_new: { singular: 'adds something new', plural: 'add something new' },
  needs_a_source: { singular: 'needs a source', plural: 'need a source' },
};

/** The phrase an unknown reaction type falls back to. */
const FALLBACK_REACTION_PHRASE: ReactionPhrase = {
  singular: 'reacted to this',
  plural: 'reacted to this',
};

/** What a comment actor did. Past tense, so both numbers read the same. */
const COMMENT_PHRASE: ReactionPhrase = {
  singular: 'commented on this',
  plural: 'commented on this',
};

/**
 * The interactions worth drawing: at most the first two, and only the two kinds
 * the API defines.
 *
 * The runtime filter matters because the response types describe the wire format
 * optimistically; an unknown `type` must not make a card render a malformed
 * sentence.
 */
export function socialProofActors(
  interactions: readonly RecentInteraction[] | undefined,
): readonly RecentInteraction[] {
  if (!Array.isArray(interactions)) {
    return [];
  }

  return interactions
    .filter(
      (interaction): interaction is RecentInteraction =>
        interaction !== null &&
        typeof interaction === 'object' &&
        (interaction.type === 'reaction' || interaction.type === 'comment'),
    )
    .slice(0, SOCIAL_PROOF_MAX_ACTORS);
}

/** The verb phrase for one interaction, in both grammatical numbers. */
function actionPhrase(interaction: RecentInteraction): ReactionPhrase {
  if (interaction.type === 'comment') {
    return COMMENT_PHRASE;
  }

  const detail = typeof interaction.detail === 'string' ? interaction.detail.trim() : '';
  return REACTION_PHRASES[detail] ?? FALLBACK_REACTION_PHRASE;
}

/**
 * The key two interactions must share to be grouped under one verb. A comment is
 * its own key; a reaction is keyed by its signal, so two DIFFERENT signals read
 * as two clauses rather than being merged into one inaccurate verb.
 */
function actionKey(interaction: RecentInteraction): string {
  if (interaction.type === 'comment') {
    return 'comment';
  }

  const detail = typeof interaction.detail === 'string' ? interaction.detail.trim() : '';
  return `reaction:${detail}`;
}

/** The name to draw for an actor; a nameless account still reads grammatically. */
function actorName(interaction: RecentInteraction): string {
  const name =
    typeof interaction.actor_display_name === 'string' ? interaction.actor_display_name.trim() : '';
  return name === '' ? 'Someone' : name;
}

/**
 * Formats the interaction header's sentence as styled runs.
 *
 * The four shapes the brief fixes (KNOT-029):
 *
 *   One reaction:        {Name} finds this true.
 *                        {Name} knows this differently.
 *                        {Name} adds something new.
 *                        {Name} needs a source.
 *   One comment:         {Name} commented on this.
 *   Two, same action:    {A} & {B} know this differently.
 *   Two, mixed actions:  {A} finds this true and {B} commented on this.
 *
 * "Same action" means both are comments, or both are reactions of the SAME
 * signal — two different signals read as two clauses rather than being merged
 * into one inaccurate verb. The verb carries its own object, so no phrase ends
 * with a dangling "this".
 *
 * Names are bold runs; every other run is plain, so the caller can give the
 * sentence its two inks without parsing it again.
 */
export function socialProofRuns(
  interactions: readonly RecentInteraction[],
): readonly SocialProofRun[] {
  const actors = socialProofActors(interactions);
  if (actors.length === 0) {
    return [];
  }

  const first = actors[0];
  const firstName = actorName(first);

  if (actors.length === 1) {
    return [
      { text: firstName, bold: true },
      { text: ` ${actionPhrase(first).singular}.`, bold: false },
    ];
  }

  const second = actors[1];
  const secondName = actorName(second);
  const sameAction = actionKey(first) === actionKey(second);

  if (sameAction) {
    return [
      { text: firstName, bold: true },
      { text: ' & ', bold: false },
      { text: secondName, bold: true },
      { text: ` ${actionPhrase(first).plural}.`, bold: false },
    ];
  }

  return [
    { text: firstName, bold: true },
    { text: ` ${actionPhrase(first).singular} and `, bold: false },
    { text: secondName, bold: true },
    { text: ` ${actionPhrase(second).singular}.`, bold: false },
  ];
}

/**
 * The attribution runs for a version with no interactions: the server's sentence
 * when it sent one, otherwise a sentence built from the structured lineage.
 *
 * This is the same wording the card drew before KNOT-028, so a quiet entry reads
 * exactly as it did.
 */
export function attributionRuns(
  attributionLine: string | undefined,
  adapterName: string,
  rootAuthorName: string,
): readonly AttributionSegment[] {
  const sentence = typeof attributionLine === 'string' ? attributionLine.trim() : '';
  if (sentence !== '') {
    return attributionSegments(sentence);
  }

  if (rootAuthorName === '') {
    return [];
  }
  if (adapterName === '') {
    return [
      { text: 'Adapted from ', bold: false },
      { text: rootAuthorName, bold: true },
      { text: "'s original story.", bold: false },
    ];
  }

  return [
    { text: 'Adapted by ', bold: false },
    { text: adapterName, bold: true },
    { text: ' from ', bold: false },
    { text: rootAuthorName, bold: true },
    { text: "'s original story.", bold: false },
  ];
}

/**
 * Whether a version entry has anything for the header to draw: an interaction, a
 * server sentence, or a structured lineage to fall back to.
 */
export function hasSocialProof(
  interactions: readonly RecentInteraction[] | undefined,
  attributionLine: string | undefined,
  rootAuthorName: string,
): boolean {
  return (
    socialProofActors(interactions).length > 0 ||
    (typeof attributionLine === 'string' && attributionLine.trim() !== '') ||
    rootAuthorName !== ''
  );
}

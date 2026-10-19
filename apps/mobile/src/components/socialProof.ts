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

/** One actor and the actions the header credits them with. */
export type SocialProofActorGroup = {
  /** The actor's display name, trimmed; empty when the account is unresolved. */
  readonly name: string;
  /** The actor's avatar path, or null when they have none. */
  readonly avatarUrl: string | null;
  /** The actor's distinct actions, newest first. */
  readonly interactions: readonly RecentInteraction[];
};

/** The actor's display name for the sentence; a nameless account is "Someone". */
function displayActorName(name: string): string {
  return name === '' ? 'Someone' : name;
}

/** The actor's trimmed display name. */
function rawActorName(interaction: RecentInteraction): string {
  return typeof interaction.actor_display_name === 'string'
    ? interaction.actor_display_name.trim()
    : '';
}

/** The actor's trimmed avatar path, or null when they have none. */
function rawActorAvatar(interaction: RecentInteraction): string | null {
  const url =
    typeof interaction.actor_avatar_url === 'string' ? interaction.actor_avatar_url.trim() : '';
  return url === '' ? null : url;
}

/**
 * The interactions worth drawing, grouped BY ACTOR and capped at two actors.
 *
 * The latest two interactions are considered. Grouping by actor first is what
 * keeps one person's name from appearing twice: a reader who both reacted and
 * commented is one actor with two actions, not two actors (KNOT-030). The wire
 * carries no actor id, so two interactions are the same person when their display
 * name AND avatar path match.
 *
 * The runtime filter matters because the response types describe the wire format
 * optimistically; an unknown `type` must not make a card render a malformed
 * sentence. Duplicate action keys within one actor are dropped, so an actor never
 * reads "finds this true and finds this true".
 */
export function socialProofActors(
  interactions: readonly RecentInteraction[] | undefined,
): readonly SocialProofActorGroup[] {
  if (!Array.isArray(interactions)) {
    return [];
  }

  const latest = interactions
    .filter(
      (interaction): interaction is RecentInteraction =>
        interaction !== null &&
        typeof interaction === 'object' &&
        (interaction.type === 'reaction' || interaction.type === 'comment'),
    )
    .slice(0, SOCIAL_PROOF_MAX_ACTORS);

  const groups: Array<{
    name: string;
    avatarUrl: string | null;
    interactions: RecentInteraction[];
    actionKeys: Set<string>;
  }> = [];
  const indexByActor = new Map<string, number>();

  for (const interaction of latest) {
    const name = rawActorName(interaction);
    const avatarUrl = rawActorAvatar(interaction);
    const actorKey = `${name}\u0000${avatarUrl ?? ''}`;
    const existing = indexByActor.get(actorKey);

    if (existing !== undefined) {
      const group = groups[existing];
      const key = actionKey(interaction);
      if (!group.actionKeys.has(key)) {
        group.interactions.push(interaction);
        group.actionKeys.add(key);
      }
      continue;
    }

    if (groups.length >= SOCIAL_PROOF_MAX_ACTORS) {
      continue;
    }

    indexByActor.set(actorKey, groups.length);
    groups.push({
      name,
      avatarUrl,
      interactions: [interaction],
      actionKeys: new Set([actionKey(interaction)]),
    });
  }

  return groups.map((group) => ({
    name: group.name,
    avatarUrl: group.avatarUrl,
    interactions: group.interactions,
  }));
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

/**
 * Formats the interaction header's sentence as styled runs.
 *
 * The shapes (KNOT-030):
 *
 *   One actor, one action:    {Name} finds this true.
 *                             {Name} commented on this.
 *   One actor, two actions:   {Name} finds this true and commented.
 *   Two actors, same action:  {A} & {B} know this differently.
 *   Two actors, different:    {A} finds this true and {B} commented on this.
 *
 * Actors are grouped FIRST, so a person who both reacted and commented is named
 * once. Within one actor a reaction always leads, because its phrase carries
 * "this"; the comment then reads simply "commented", so the object is not said
 * twice. Two actors are joined with "&" when they share an action and with "and"
 * when they do not. Names are bold runs; every other run is plain.
 */
export function socialProofRuns(
  interactions: readonly RecentInteraction[] | undefined,
): readonly SocialProofRun[] {
  const actors = socialProofActors(interactions);
  if (actors.length === 0) {
    return [];
  }
  if (actors.length === 1) {
    return singleActorRuns(actors[0]);
  }

  const first = actors[0];
  const second = actors[1];
  const firstAction = first.interactions[0];
  const secondAction = second.interactions[0];
  const firstName = displayActorName(first.name);
  const secondName = displayActorName(second.name);

  if (actionKey(firstAction) === actionKey(secondAction)) {
    return [
      { text: firstName, bold: true },
      { text: ' & ', bold: false },
      { text: secondName, bold: true },
      { text: ` ${actionPhrase(firstAction).plural}.`, bold: false },
    ];
  }

  return [
    { text: firstName, bold: true },
    { text: ` ${actionPhrase(firstAction).singular} and `, bold: false },
    { text: secondName, bold: true },
    { text: ` ${actionPhrase(secondAction).singular}.`, bold: false },
  ];
}

/**
 * One actor's sentence. A single action reads normally; two actions share the
 * name once and join with "and". A reaction leads when both exist, because its
 * phrase carries the object ("finds this true") and the comment can then read
 * simply "commented" (KNOT-030).
 */
function singleActorRuns(actor: SocialProofActorGroup): readonly SocialProofRun[] {
  const name = displayActorName(actor.name);
  const actions = actor.interactions;

  if (actions.length === 1) {
    return [
      { text: name, bold: true },
      { text: ` ${actionPhrase(actions[0]).singular}.`, bold: false },
    ];
  }

  const ordered = [
    ...actions.filter((action) => action.type === 'reaction'),
    ...actions.filter((action) => action.type === 'comment'),
  ];
  const first = ordered[0];
  const second = ordered[1];
  const secondPhrase = second.type === 'comment' ? 'commented' : actionPhrase(second).singular;

  return [
    { text: name, bold: true },
    { text: ` ${actionPhrase(first).singular} and ${secondPhrase}.`, bold: false },
  ];
}

/**
 * The attribution runs for a version with no interactions: the server's sentence
 * when it sent one, otherwise a sentence built from the structured lineage.
 *
 * The adapter is deliberately absent — their name is the AuthorLine below the
 * header — so the fallback matches the server's concise wording (KNOT-030).
 */
export function attributionRuns(
  attributionLine: string | undefined,
  rootAuthorName: string,
): readonly AttributionSegment[] {
  const sentence = typeof attributionLine === 'string' ? attributionLine.trim() : '';
  if (sentence !== '') {
    return attributionSegments(sentence);
  }

  if (rootAuthorName === '') {
    return [];
  }

  return [
    { text: 'Adapted from ', bold: false },
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

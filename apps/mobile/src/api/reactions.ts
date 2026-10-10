/**
 * The reaction endpoints of the Knot HTTP API: the four perspective signals a
 * reader can leave on a story, a version, a comment, or a bridge.
 *
 * A reaction is a perspective signal, not a like (KNOT-ADR-050). The four signals
 * do not compete: a reader may hold any combination of them on the same entity,
 * and none of them cancels another. They never sort or rank content.
 *
 * Everything here goes through the shared transport in `client.ts`, so error
 * handling is identical to the other endpoints: a failure is always an `ApiError`
 * carrying the server's machine-readable code.
 */
import { request } from './client';

/** The kinds of content a reaction can target. */
export type ReactionEntityType = 'story' | 'version' | 'comment' | 'bridge';

/** The four perspective signals. The set is closed: the server's schema refuses anything else. */
export type ReactionType =
  'rings_true' | 'know_it_differently' | 'adds_something_new' | 'needs_a_source';

/** The four signals in display order. */
export const REACTION_TYPES: readonly ReactionType[] = [
  'rings_true',
  'know_it_differently',
  'adds_something_new',
  'needs_a_source',
];

/** The emoji each signal is drawn with, per the locked iconography decision. */
export const REACTION_EMOJI: Readonly<Record<ReactionType, string>> = {
  rings_true: '✅',
  know_it_differently: '🔄',
  adds_something_new: '➕',
  needs_a_source: '📎',
};

/** The label each signal is drawn with. */
export const REACTION_LABELS: Readonly<Record<ReactionType, string>> = {
  rings_true: 'Rings true',
  know_it_differently: 'Know it differently',
  adds_something_new: 'Adds something new',
  needs_a_source: 'Needs a source',
};

/**
 * The counts of each signal on one entity. All four keys are always present; zero
 * is a real answer (no one left that signal).
 */
export type ReactionCounts = {
  readonly rings_true: number;
  readonly know_it_differently: number;
  readonly adds_something_new: number;
  readonly needs_a_source: number;
};

/** A summary with no reactions, used when a response omits the field. */
export const EMPTY_REACTION_COUNTS: ReactionCounts = {
  rings_true: 0,
  know_it_differently: 0,
  adds_something_new: 0,
  needs_a_source: 0,
};

/** The user who left a reaction, as a reaction list renders them. */
export type ReactionActor = {
  readonly id: string;
  readonly display_name: string;
  /** A path on the API, or "" when they have no avatar. */
  readonly avatar_url: string;
};

/** One entry of a reaction list. */
export type ReactionItem = {
  readonly id: string;
  readonly user: ReactionActor;
  readonly reaction_type: ReactionType;
  readonly created_at: string;
};

/** The body returned by a toggle: the entity's updated counts. */
export type ToggleReactionResponse = {
  readonly reactions: ReactionCounts;
};

/** The body returned by a reaction list. */
export type ReactionListResponse = {
  readonly reactions: readonly ReactionItem[];
};

/**
 * The path segment for each entity kind. The toggle and list routes are
 * per-entity (`/stories/{id}/reactions`), so the kind selects the collection.
 */
const ENTITY_PATH: Readonly<Record<ReactionEntityType, string>> = {
  story: 'stories',
  version: 'versions',
  comment: 'comments',
  bridge: 'bridges',
};

/** The reaction endpoints. */
export const reactionsApi = {
  /**
   * POST /{entity}/{id}/reactions — toggles one signal on one entity.
   *
   * The reactor is taken from `token` by the server; it is never sent in the
   * body. Toggling a signal the reader already holds removes it; otherwise it is
   * added. The response carries the entity's updated counts.
   */
  toggleReaction(
    entityType: ReactionEntityType,
    entityId: string,
    reactionType: ReactionType,
    token: string,
  ): Promise<ToggleReactionResponse> {
    return request<ToggleReactionResponse>(
      `/${ENTITY_PATH[entityType]}/${encodeURIComponent(entityId)}/reactions`,
      { method: 'POST', body: { reaction_type: reactionType }, token },
    );
  },

  /**
   * GET /{entity}/{id}/reactions — every reaction on one entity, newest first,
   * with each reactor resolved. Public.
   */
  getReactions(entityType: ReactionEntityType, entityId: string): Promise<ReactionListResponse> {
    return request<ReactionListResponse>(
      `/${ENTITY_PATH[entityType]}/${encodeURIComponent(entityId)}/reactions`,
      { method: 'GET' },
    );
  },
};

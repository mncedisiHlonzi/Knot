/**
 * The inbox's sentence logic, kept apart from its screen.
 *
 * Nothing here touches React Native, so the rules that turn a notification into
 * the sentence it reads as can be tested directly — the same split the discovery
 * screens use for their labels (KNOT-020e). The screen keeps fetching, paging and
 * layout; this module keeps the wording.
 */
import type { Notification } from '../../api/notifications';
import { REACTION_EMOJI, REACTION_LABELS, ReactionType } from '../../api/reactions';
import { API_BASE_URL } from '../../config/api';

/** One inbox sentence, split into the actor and the clause that follows the name. */
export type NotificationSentence = {
  /** The actor's name, or "Someone" when their account no longer resolves. */
  readonly actor: string;
  /** The clause naming the act. Always ends in a full stop. */
  readonly text: string;
};

/**
 * Names a reaction signal for the inbox sentence: its emoji and label, e.g.
 * "✅ Rings true". An unknown or absent type falls back to a generic phrase, so a
 * row from a newer server never reads as broken.
 */
export function reactionPhrase(raw: string | undefined): string {
  if (raw !== undefined && raw in REACTION_LABELS) {
    const type = raw as ReactionType;
    return `${REACTION_EMOJI[type]} ${REACTION_LABELS[type]}`;
  }
  return 'reacted';
}

/** The absolute URL for an actor's avatar, or undefined when they have none. */
export function avatarUri(avatarUrl: string): string | undefined {
  const path = avatarUrl.trim();
  return path === '' ? undefined : `${API_BASE_URL}${path}`;
}

/**
 * Splits a notification into the sentence it reads as: the actor who acted, and
 * the clause naming the act.
 *
 * The two are returned apart — where the pre-design-pass helper returned one
 * string — so the row can draw the actor's name in the stronger ink while the
 * clause stays in the body ink, which is how the design reference sets the line.
 * Every sentence reads exactly as the inbox already read it.
 *
 * An actor the server could not resolve is named "Someone" rather than being
 * hidden, so a row never loses its meaning when an account is deleted.
 */
export function notificationParts(notification: Notification): NotificationSentence {
  const actor = notification.actor?.display_name ?? 'Someone';

  switch (notification.event_type) {
    case 'version.created':
      return { actor, text: 'adapted your version.' };
    case 'comment.created':
      return { actor, text: 'commented on your version.' };
    case 'reaction.created':
      return {
        actor,
        text: `${reactionPhrase(notification.reaction_type)} on your ${notification.entity_type}.`,
      };
    case 'inquiry.answered':
      return { actor, text: 'answered your question.' };
    case 'inquiry.nearby':
      // The one event about content the recipient has no part in: it fires because
      // of where they are from, so the sentence names the act, not "your".
      return { actor, text: 'asked a question about your place.' };
    default:
      return { actor, text: 'did something on your content.' };
  }
}

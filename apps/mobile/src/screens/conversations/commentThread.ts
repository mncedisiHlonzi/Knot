/**
 * The pure rules behind a comment thread: how a flat page of comments is grouped
 * into top-level comments and their replies, which comment a reply attaches to,
 * what the expand/collapse link reads, how a language is labelled on the
 * composer's chip, and which language the composer starts in.
 *
 * They live outside the screen so the rules can be tested without rendering
 * React Native, and so the screen stays about layout and requests.
 */
import type { Comment } from '../../api/conversations';
import { isLanguageCode, languageName } from '../../data/languages';

/**
 * The language the comment box falls back to when neither the caller nor the
 * version names one the app knows.
 */
export const DEFAULT_COMPOSE_LANGUAGE = 'eng';

/**
 * The widest language name the chip shows before falling back to the code.
 *
 * 1,630 of the 7,927 canonical names are longer than this, so the chip would
 * otherwise wrap or crowd out the post button. A code is always 3 characters and
 * is what the server stores, so falling back to it loses no meaning.
 */
export const MAX_CHIP_LENGTH = 12;

/**
 * The comment a reply to `comment` attaches to.
 *
 * Threading is one level deep (KNOT-ADR-047), so replying to a reply attaches to
 * that reply's top-level comment, not to the reply: the server applies the same
 * rule, and asking it here keeps the two from disagreeing.
 */
export function replyTargetId(comment: Pick<Comment, 'id' | 'parent_comment_id'>): string {
  return comment.parent_comment_id ?? comment.id;
}

/** Reports whether a comment is a reply, so the thread can indent it. */
export function isReply(comment: Pick<Comment, 'parent_comment_id'>): boolean {
  return comment.parent_comment_id !== null;
}

/**
 * How many replies a collapsed comment reveals on its first tap.
 *
 * A comment with more than this shows a "View all N replies" link while expanded,
 * and revealing the rest takes a second tap (KNOT-ADR-049).
 */
export const REPLIES_PREVIEW_COUNT = 3;

/** A page of comments split into what the thread renders as parents and replies. */
export type GroupedComments = {
  /**
   * The top-level comments, in the order the API returned them (newest first).
   * These are the rows of the list; each one owns its replies.
   */
  readonly topLevel: Comment[];
  /**
   * The replies of each top-level comment, keyed by the parent's id and in the
   * order the API returned them (oldest first). A parent with no replies has no
   * entry, so a missing key and an empty array both mean "no replies".
   */
  readonly repliesByParent: Record<string, Comment[]>;
};

/**
 * Splits a flat page of comments into its top-level comments and their replies.
 *
 * The API already returns a renderable flat list — each top-level comment
 * followed by its replies — so this only re-shapes it: the order of every row is
 * preserved, and no comment is re-sorted. A comment whose parent is not in the
 * page is still grouped under that parent id, so it is never dropped; the thread
 * simply does not render it until the parent is present.
 *
 * `parent_comment_id` is compared against null to decide the level, and a
 * missing value is treated as top-level too, because the API types describe the
 * wire format optimistically and a response can omit the field (KNOT-015b-fix).
 */
export function groupComments(comments: readonly Comment[]): GroupedComments {
  const topLevel: Comment[] = [];
  const repliesByParent: Record<string, Comment[]> = {};

  for (const comment of comments) {
    const parentId = comment.parent_comment_id;
    if (parentId === null || parentId === undefined) {
      topLevel.push(comment);
      continue;
    }

    const replies = repliesByParent[parentId];
    if (replies === undefined) {
      repliesByParent[parentId] = [comment];
    } else {
      replies.push(comment);
    }
  }

  return { topLevel, repliesByParent };
}

/**
 * The label for a comment's expand/collapse link, or null when it has no replies.
 *
 * The link cycles through three states for a comment with more than
 * `REPLIES_PREVIEW_COUNT` replies: collapsed shows "View all N replies", the
 * first tap reveals the first three and shows "View all N replies" again, and the
 * second tap reveals the rest and shows "Hide replies". A comment with three or
 * fewer replies is fully revealed by its first tap, so it goes straight to "Hide
 * replies" (KNOT-ADR-049).
 *
 * @param total          how many replies the comment has
 * @param expanded       whether its replies are currently visible
 * @param fullyExpanded  whether all of its replies are visible (past the preview)
 */
export function repliesLinkLabel(
  total: number,
  expanded: boolean,
  fullyExpanded: boolean,
): string | null {
  if (total <= 0) {
    return null;
  }

  if (expanded) {
    const allShown = total <= REPLIES_PREVIEW_COUNT || fullyExpanded;
    return allShown ? 'Hide replies' : `View all ${total} replies`;
  }

  if (total <= REPLIES_PREVIEW_COUNT) {
    return total === 1 ? 'View 1 reply' : `View ${total} replies`;
  }

  return `View all ${total} replies`;
}

/**
 * The label for a language chip: the name when it fits, otherwise the code.
 *
 * An empty code has no label at all — the chip then shows its placeholder.
 */
export function languageChipLabel(code: string): string {
  const trimmed = code.trim();
  if (trimmed === '') {
    return '';
  }

  const name = languageName(trimmed);
  return name.length <= MAX_CHIP_LENGTH ? name : trimmed;
}

/**
 * The language the composer starts in: the caller's suggestion when it is a code
 * the app knows, otherwise `DEFAULT_COMPOSE_LANGUAGE`.
 *
 * The suggestion is the user's first preferred language, which the caller passes
 * in. When the user has none, the version's own language is a better default;
 * that needs a request, so the screen resolves it and calls this again.
 */
export function initialComposeLanguage(preferred?: string, versionLanguage?: string): string {
  const candidates = [preferred, versionLanguage];
  for (const candidate of candidates) {
    const trimmed = (candidate ?? '').trim();
    if (isLanguageCode(trimmed)) {
      return trimmed;
    }
  }

  return DEFAULT_COMPOSE_LANGUAGE;
}

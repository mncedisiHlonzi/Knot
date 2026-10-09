/**
 * The pure rules behind a comment thread: which comment a reply attaches to, how
 * a language is labelled on the composer's chip, and which language the composer
 * starts in.
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

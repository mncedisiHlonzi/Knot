/**
 * Pure helpers for the Curious Inquiries screens.
 *
 * They are kept out of the screens so they can be unit-tested without a renderer
 * (the Jest environment here is `node`, so a `.tsx` component test would not even
 * be collected; see repo memory). Nothing here touches React or the network.
 */

/** The longest title the server accepts, after trimming. Mirrors the Go rule. */
export const INQUIRY_TITLE_MAX_LENGTH = 200;

/** The longest question body the server accepts, after trimming. */
export const INQUIRY_BODY_MAX_LENGTH = 5000;

/** The longest answer the server accepts, after trimming. */
export const ANSWER_BODY_MAX_LENGTH = 5000;

/** The longest place the server accepts, after trimming. */
export const INQUIRY_PLACE_MAX_LENGTH = 100;

/** How many Rooted people the server routes a new question to. Mirrors the Go rule. */
export const NEARBY_RECIPIENT_LIMIT = 5;

/**
 * How many answers an inquiry has, in words.
 *
 * A brand-new question says so rather than showing "0 answers", because an
 * inquiry with no answers is the normal state of a question just asked, not an
 * empty result.
 */
export function answerCountLabel(count: number): string {
  if (!Number.isFinite(count) || count <= 0) {
    return 'No answers yet';
  }
  return count === 1 ? '1 answer' : `${count} answers`;
}

/**
 * The place a question is about, as a line of text.
 *
 * A place-less inquiry is normal — it is simply reached through the list rather
 * than routed to anyone — so it reads as a statement, not as a missing value.
 */
export function inquiryPlaceLabel(place: string | null | undefined): string {
  const trimmed = typeof place === 'string' ? place.trim() : '';
  return trimmed === '' ? 'No place named' : `About ${trimmed}`;
}

/**
 * Who a new question will be announced to, given the place it names.
 *
 * It describes the routing honestly: the first few people Rooted in the place are
 * told about it and everyone else reaches it through the public list, so a person
 * asking a question knows what asking actually does (KNOT-ADR-056).
 */
export function inquiryAudienceLabel(place: string | null | undefined): string {
  const trimmed = typeof place === 'string' ? place.trim() : '';
  if (trimmed === '') {
    return 'No place named, so nobody is notified. Your question is still public in the Inquiries list.';
  }
  return `The first ${NEARBY_RECIPIENT_LIMIT} people rooted in ${trimmed} are notified. Everyone else finds it in the Inquiries list.`;
}

/** What the ask form currently holds. */
export type InquiryDraft = {
  readonly title: string;
  readonly body: string;
  readonly language: string;
};

/**
 * The first thing wrong with a draft, or undefined when it is ready to send.
 *
 * The rules mirror the server's, so an obvious mistake is reported without a
 * round trip; the server remains the authority and its own message is shown if it
 * disagrees.
 */
export function inquiryDraftError(draft: InquiryDraft): string | undefined {
  const title = draft.title.trim();
  if (title === '') {
    return 'A question title is required.';
  }
  if (title.length > INQUIRY_TITLE_MAX_LENGTH) {
    return `The title must be at most ${INQUIRY_TITLE_MAX_LENGTH} characters.`;
  }

  const body = draft.body.trim();
  if (body === '') {
    return 'A question is required.';
  }
  if (body.length > INQUIRY_BODY_MAX_LENGTH) {
    return `The question must be at most ${INQUIRY_BODY_MAX_LENGTH} characters.`;
  }

  if (draft.language.trim() === '') {
    return 'A language is required.';
  }

  return undefined;
}

/** The first thing wrong with an answer draft, or undefined when it is ready. */
export function answerDraftError(body: string): string | undefined {
  const trimmed = body.trim();
  if (trimmed === '') {
    return 'An answer is required.';
  }
  if (trimmed.length > ANSWER_BODY_MAX_LENGTH) {
    return `The answer must be at most ${ANSWER_BODY_MAX_LENGTH} characters.`;
  }
  return undefined;
}

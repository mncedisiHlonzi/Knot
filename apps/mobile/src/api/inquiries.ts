/**
 * The Curious Inquiries endpoints of the Knot HTTP API.
 *
 * An inquiry is a question someone asks about a place, answered publicly by the
 * people who know it. Everything here goes through the shared transport in
 * `client.ts`, so error handling is identical to the story, conversation, and
 * profile endpoints: a failure is always an `ApiError` carrying the server's
 * machine-readable code.
 *
 * Two rules shape the wire format and the UI:
 *
 *  - every inquiry and every answer is public and attributed, so both always
 *    carry their author's id, display name, avatar path, and Rooted summary —
 *    there is no anonymous variant to render;
 *  - an inquiry stays open forever, so there is no "accepted" answer and no state
 *    to show beyond `answer_count` (KNOT-ADR-055).
 */
import { request } from './client';
import type { ReactionCounts } from './reactions';
import type { AuthorRooted } from './rooted';

/** The page size the inquiry list asks for. */
export const INQUIRY_PAGE_SIZE = 20;

/** The page size an answer thread asks for. The server defaults to 50, caps at 100. */
export const ANSWER_PAGE_SIZE = 50;

/** An inquiry as returned by the API. */
export type Inquiry = {
  readonly id: string;
  /** The asker. Every inquiry is attributed, so this is never null. */
  readonly author_id: string;
  readonly title: string;
  readonly body: string;
  /** The ISO 639-3 code the question is written in. */
  readonly language: string;
  /** The place asked about, or null when the question names none. */
  readonly place: string | null;
  readonly place_country: string | null;
  readonly latitude: number | null;
  readonly longitude: number | null;
  /** How many public answers the question holds. */
  readonly answer_count: number;
  readonly created_at: string;
  readonly updated_at: string;
  /**
   * The asker's inline attribution. Optional because the API types describe the
   * wire format optimistically: a response from an older server omits them, and a
   * client must treat a missing field as absent rather than assume a string
   * (KNOT-015b-fix).
   */
  readonly author_display_name?: string;
  readonly author_avatar_url?: string | null;
  readonly author_rooted?: AuthorRooted | null;
  /** The perspective signals on the question, and the reader's own. */
  readonly reactions?: ReactionCounts;
  readonly my_reactions?: readonly string[];
};

/** An answer as returned by the API. */
export type InquiryAnswer = {
  readonly id: string;
  readonly inquiry_id: string;
  /** The answerer. Every answer is attributed, so this is never null. */
  readonly author_id: string;
  readonly language: string;
  readonly body: string;
  readonly created_at: string;
  readonly updated_at: string;
  readonly author_display_name?: string;
  readonly author_avatar_url?: string | null;
  readonly author_rooted?: AuthorRooted | null;
};

/** The request body for POST /inquiries. The asker comes from the token. */
export type CreateInquiryPayload = {
  readonly title: string;
  readonly body: string;
  readonly language: string;
  /** Optional. A place is what routes the question to that place's Rooted users. */
  readonly place?: string;
  /** Structured place data from the location picker; the pair goes together. */
  readonly latitude?: number;
  readonly longitude?: number;
  readonly place_country?: string;
};

/** The request body for POST /inquiries/{id}/answers. */
export type CreateAnswerPayload = {
  readonly body: string;
  readonly language: string;
};

/** The body returned by POST /inquiries and GET /inquiries/{id}. */
export type InquiryResponse = {
  readonly inquiry: Inquiry;
};

/** The body returned by POST /inquiries/{id}/answers and GET /answers/{id}. */
export type AnswerResponse = {
  readonly answer: InquiryAnswer;
};

/** The body returned by GET /inquiries. */
export type InquiryListResponse = {
  readonly inquiries: readonly Inquiry[];
  /** The token that resumes the list after this page, or "" at the end. */
  readonly next_cursor: string;
};

/** The body returned by GET /inquiries/{id}/answers. */
export type AnswerListResponse = {
  readonly answers: readonly InquiryAnswer[];
  readonly next_cursor: string;
};

/** Options for an inquiry page. */
export type ListInquiriesOptions = {
  /**
   * A cursor from a previous page. Omit it to start at the newest inquiry.
   */
  readonly cursor?: string;
  /** How many to ask for. The server defaults to 20 and caps at 50. */
  readonly limit?: number;
  /**
   * Restrict the page to one place, matched exactly on the stored spelling. Omit
   * it for every place, including questions that name none.
   */
  readonly place?: string;
};

/** Options for an answer page. */
export type ListAnswersOptions = {
  readonly cursor?: string;
  /** How many to ask for. The server defaults to 50 and caps at 100. */
  readonly limit?: number;
};

/**
 * Builds a query string, omitting empty values so the server applies its own
 * defaults.
 */
function queryString(parts: readonly (readonly [string, string | number | undefined])[]): string {
  const encoded: string[] = [];

  for (const [key, value] of parts) {
    if (value === undefined || value === '') {
      continue;
    }
    encoded.push(`${key}=${encodeURIComponent(String(value))}`);
  }

  return encoded.length === 0 ? '' : `?${encoded.join('&')}`;
}

/** The Curious Inquiries endpoints. */
export const inquiriesApi = {
  /**
   * POST /inquiries — asks a question about a place.
   *
   * Requires an access token. A question with a place is routed to the first
   * Rooted users of that place by the server; the client does not choose readers.
   */
  createInquiry(token: string, payload: CreateInquiryPayload): Promise<InquiryResponse> {
    return request<InquiryResponse>('/inquiries', { method: 'POST', body: payload, token });
  },

  /**
   * GET /inquiries — one page of open questions, newest first. Public.
   *
   * A signed-in caller also receives their own reaction highlights; an anonymous
   * caller does not, and the request still succeeds.
   */
  listInquiries(options: ListInquiriesOptions = {}, token?: string): Promise<InquiryListResponse> {
    const query = queryString([
      ['place', options.place],
      ['limit', options.limit],
      ['cursor', options.cursor],
    ]);

    return request<InquiryListResponse>(`/inquiries${query}`, { method: 'GET', token });
  },

  /** GET /inquiries/{id} — one question. Public. */
  getInquiry(id: string, token?: string): Promise<InquiryResponse> {
    return request<InquiryResponse>(`/inquiries/${encodeURIComponent(id)}`, {
      method: 'GET',
      token,
    });
  },

  /**
   * POST /inquiries/{id}/answers — answers a question publicly.
   *
   * Requires an access token. The server notifies the asker; answering your own
   * question does not notify you.
   */
  createAnswer(
    token: string,
    inquiryId: string,
    payload: CreateAnswerPayload,
  ): Promise<AnswerResponse> {
    return request<AnswerResponse>(`/inquiries/${encodeURIComponent(inquiryId)}/answers`, {
      method: 'POST',
      body: payload,
      token,
    });
  },

  /**
   * GET /inquiries/{id}/answers — one page of a question's answers, OLDEST first,
   * so the thread reads as a conversation. Public.
   *
   * An unknown inquiry is a 404 rather than an empty thread, so the caller can
   * tell "no such question" from "nobody has answered yet".
   */
  listAnswers(
    inquiryId: string,
    options: ListAnswersOptions = {},
    token?: string,
  ): Promise<AnswerListResponse> {
    const query = queryString([
      ['limit', options.limit],
      ['cursor', options.cursor],
    ]);

    return request<AnswerListResponse>(
      `/inquiries/${encodeURIComponent(inquiryId)}/answers${query}`,
      { method: 'GET', token },
    );
  },

  /**
   * GET /answers/{id} — one answer. Public.
   *
   * The top-level path (not `/inquiries/answers/{id}`) matches how a single
   * comment and a single bridge are already fetched.
   */
  getAnswer(id: string, token?: string): Promise<AnswerResponse> {
    return request<AnswerResponse>(`/answers/${encodeURIComponent(id)}`, { method: 'GET', token });
  },
};

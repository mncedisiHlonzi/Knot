/**
 * The search endpoint of the Knot HTTP API.
 *
 * One request searches the three content types that carry text — stories
 * (matched through their root version), adaptations, and inquiries — and returns
 * a single ranked list. As with every other endpoint, a failure is always an
 * `ApiError` carrying the server's machine-readable code, because everything goes
 * through the shared transport in `client.ts`.
 *
 * The endpoint is public: it needs no token. A token is sent anyway so the server
 * can hide blocked authors' content for a signed-in reader (KNOT-ADR-064); an
 * anonymous request simply sees everything.
 */
import { request } from './client';
import type { AuthorRooted } from './rooted';
import type { Pillar } from './stories';

/** The page size the search screen asks for. The server defaults to 20, caps at 50. */
export const SEARCH_PAGE_SIZE = 20;

/**
 * The shortest phrase the server accepts, counted in characters. A one-character
 * phrase matches almost everything, so it is refused rather than served; the
 * server allows an empty phrase only when a filter narrows the search.
 */
export const MIN_SEARCH_LENGTH = 2;

/** Which corpus a result came from. */
export type SearchKind = 'story' | 'version' | 'inquiry';

/** One ranked search result. */
export type SearchResult = {
  readonly kind: SearchKind;
  /** The story id, the version id, or the inquiry id — whatever the result is. */
  readonly id: string;
  /** The story a version belongs to, or null for a story or an inquiry. */
  readonly story_id: string | null;
  readonly title: string;
  /** The beginning of the body, already truncated by the server. */
  readonly preview: string;
  /** The ISO 639-3 code the text is written in. */
  readonly language: string;
  /** The story's pillar, or null for an inquiry, which has no pillar. */
  readonly pillar: Pillar | null;
  /** The story's place or the inquiry's place, or null when none was recorded. */
  readonly place: string | null;
  readonly author_id: string;
  readonly created_at: string;
  /**
   * The author's inline attribution. Optional because the API types describe the
   * wire format optimistically: a response from an older server omits them, and a
   * client must treat a missing field as absent rather than assume a string
   * (KNOT-015b-fix).
   */
  readonly author_display_name?: string;
  readonly author_avatar_url?: string | null;
  readonly author_rooted?: AuthorRooted | null;
};

/** The body returned by GET /search. */
export type SearchResponse = {
  readonly results: readonly SearchResult[];
  /** The token that resumes the list after this page, or "" at the end. */
  readonly next_cursor: string;
};

/** A search request. Every field is optional; the server refuses a request with neither a phrase nor a filter. */
export type SearchParams = {
  /** The phrase to match. May be omitted only when a filter is set. */
  readonly q?: string;
  /** Restrict the search to one content type. */
  readonly type?: SearchKind;
  /** Restrict the search to one pillar. Excludes inquiries, which have no pillar. */
  readonly pillar?: Pillar;
  /** Restrict the search to one ISO 639-3 code. */
  readonly language?: string;
  /** Restrict the search to one author. */
  readonly authorId?: string;
  /** Restrict the search to one place, matched exactly on the stored spelling. */
  readonly place?: string;
  /** A cursor from a previous page. Omit it to start at the best match. */
  readonly cursor?: string;
  /** How many results to ask for. */
  readonly limit?: number;
};

/**
 * Reports whether the server will accept these parameters.
 *
 * The rule mirrors the server's own: a phrase of at least `MIN_SEARCH_LENGTH`
 * characters, or at least one filter. The screen uses it to decide whether to
 * issue a request at all, so typing a single character (or clearing the field)
 * neither searches nor surfaces a validation error.
 *
 * The phrase is measured in **code points**, not UTF-16 code units, so an emoji or
 * a multi-byte letter counts as the one character the server counts — counting
 * `"👍".length` would report 2 and send a request the server rejects.
 */
export function canSearch(params: SearchParams): boolean {
  const phrase = (params.q ?? '').trim();
  if ([...phrase].length >= MIN_SEARCH_LENGTH) {
    return true;
  }

  return Boolean(
    (params.type ?? '') !== '' ||
    (params.pillar ?? '') !== '' ||
    (params.language ?? '') !== '' ||
    (params.authorId ?? '') !== '' ||
    (params.place ?? '') !== '',
  );
}

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

/** The search endpoint. */
export const searchApi = {
  /**
   * GET /search — one page of ranked results. Public.
   *
   * The token is optional and only ever narrows the response: it lets the server
   * hide content authored by someone the reader has blocked.
   */
  search(params: SearchParams = {}, token?: string): Promise<SearchResponse> {
    const query = queryString([
      ['q', params.q],
      ['type', params.type],
      ['pillar', params.pillar],
      ['language', params.language],
      ['author_id', params.authorId],
      ['place', params.place],
      ['cursor', params.cursor],
      ['limit', params.limit],
    ]);

    return request<SearchResponse>(`/search${query}`, { method: 'GET', token });
  },
};

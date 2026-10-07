/**
 * The story endpoints of the Knot HTTP API.
 *
 * Everything here goes through the shared transport in `client.ts`, so error
 * handling is identical to the auth endpoints: a failure is always an `ApiError`
 * carrying the server's machine-readable code.
 */
import { request } from './client';

/** The two storytelling lenses a story can be filed under. */
export type Pillar = 'wonder' | 'heritage';

/** The pillar values, in the order the UI offers them. */
export const PILLARS: readonly Pillar[] = ['wonder', 'heritage'];

/** A story as returned by the API. */
export type Story = {
  readonly id: string;
  readonly author_id: string;
  /**
   * The id of the story's root version. The content fields below (language,
   * title, body) are the root version's content; adapting a story starts from
   * this version.
   */
  readonly root_version_id: string;
  readonly pillar: Pillar;
  readonly language: string;
  readonly title: string;
  readonly body: string;
  readonly approximate_location: string;
  readonly media_urls: readonly string[];
  readonly sensitive: boolean;
  readonly created_at: string;
  readonly updated_at: string;
};

/** The request body for POST /stories. */
export type CreateStoryPayload = {
  readonly pillar: Pillar;
  readonly language: string;
  readonly title: string;
  readonly body: string;
  readonly approximate_location?: string;
  readonly media_urls?: readonly string[];
  readonly sensitive?: boolean;
};

/** The body returned by POST /stories and GET /stories/{id}. */
export type StoryResponse = {
  readonly story: Story;
};

/** The body returned by GET /stories. */
export type StoryListResponse = {
  readonly stories: readonly Story[];
  /**
   * The token that resumes the feed after this page, or an empty string when
   * there are no more pages. The server always sends the field.
   */
  readonly next_cursor: string;
};

/** Options for a feed page. */
export type ListStoriesOptions = {
  /** A cursor from a previous page. Omit it to start at the newest story. */
  readonly cursor?: string;
  /** How many stories to ask for. The server defaults to 20 and caps at 50. */
  readonly limit?: number;
};

/** The page size the feed asks for. */
export const FEED_PAGE_SIZE = 20;

/**
 * Builds the query string for a feed request, omitting empty values so the
 * server applies its own defaults.
 */
function feedQuery(options: ListStoriesOptions): string {
  const parts: string[] = [];

  if (options.limit !== undefined) {
    parts.push(`limit=${encodeURIComponent(String(options.limit))}`);
  }
  if (options.cursor !== undefined && options.cursor !== '') {
    parts.push(`cursor=${encodeURIComponent(options.cursor)}`);
  }

  return parts.length === 0 ? '' : `?${parts.join('&')}`;
}

/** The story endpoints. */
export const storiesApi = {
  /**
   * POST /stories — publishes a story as the authenticated user.
   *
   * The author is taken from `token` by the server; it is never sent in the body.
   */
  createStory(token: string, payload: CreateStoryPayload): Promise<StoryResponse> {
    return request<StoryResponse>('/stories', { method: 'POST', body: payload, token });
  },

  /** GET /stories/{id} — reads one story. Public. */
  getStory(id: string): Promise<StoryResponse> {
    return request<StoryResponse>(`/stories/${encodeURIComponent(id)}`, { method: 'GET' });
  },

  /** GET /stories — reads one page of the feed, newest first. Public. */
  listStories(options: ListStoriesOptions = {}): Promise<StoryListResponse> {
    return request<StoryListResponse>(`/stories${feedQuery(options)}`, { method: 'GET' });
  },
};

/**
 * The conversation endpoints of the Knot HTTP API: comments on a story version.
 *
 * Everything here goes through the shared transport in `client.ts`, so error
 * handling is identical to the auth, story, and version endpoints: a failure is
 * always an `ApiError` carrying the server's machine-readable code.
 */
import { request } from './client';
import type { AuthorRooted } from './rooted';

/** One comment on a story version. */
export type Comment = {
  readonly id: string;
  readonly version_id: string;
  readonly author_id: string;
  /**
   * The author's display name and avatar path, attached by the server. The avatar
   * is a relative path on the API, or null when the author has no avatar (the
   * client then renders initials). Both are optional, because a response from an
   * older server omits them (KNOT-015b-fix).
   */
  readonly author_display_name?: string;
  readonly author_avatar_url?: string | null;
  readonly language: string;
  readonly body: string;
  /**
   * The comment this one replies to, or null for a top-level comment.
   *
   * Threading is one level deep: a reply to a reply names the same top-level
   * comment its parent does, so a thread is always a top-level comment followed
   * by its replies (KNOT-ADR-047).
   */
  readonly parent_comment_id: string | null;
  readonly created_at: string;
  readonly updated_at: string;
  /**
   * The author's primary public Rooted signal, or null when they have none. The
   * server attaches it to the comment list and create responses.
   */
  readonly author_rooted?: AuthorRooted | null;
};

/** The request body for POST /versions/{id}/comments. */
export type CreateCommentPayload = {
  readonly body: string;
  readonly language: string;
  /**
   * The comment to reply to. Omit it, or send null, for a top-level comment.
   * The server attaches a reply to a reply to that reply's top-level comment, so
   * a client only ever needs to send the id of the comment the user tapped.
   */
  readonly parent_comment_id?: string | null;
};

/** The body returned by POST /versions/{id}/comments. */
export type CommentResponse = {
  readonly comment: Comment;
};

/**
 * A comment as returned by `GET /comments/{id}`: the comment plus the id of the
 * story its version belongs to. The list and create responses carry no
 * `story_id`, because only a caller resolving a single comment needs to name the
 * thread's story.
 */
export type CommentDetail = Comment & {
  readonly story_id: string;
};

/** The body returned by GET /comments/{id}. */
export type CommentDetailResponse = {
  readonly comment: CommentDetail;
};

/** The body returned by GET /versions/{id}/comments. */
export type CommentListResponse = {
  readonly comments: readonly Comment[];
  /** The token that resumes the thread after this page, or "" at the end. */
  readonly next_cursor: string;
};

/** Options for a comment page. */
export type ListCommentsOptions = {
  /** A cursor from a previous page. Omit it to start at the newest comment. */
  readonly cursor?: string;
  /** How many comments to ask for. The server defaults to 20 and caps at 50. */
  readonly limit?: number;
};

/** The page size the thread asks for. */
export const THREAD_PAGE_SIZE = 20;

/**
 * Builds the query string for a thread request, omitting empty values so the
 * server applies its own defaults.
 */
function threadQuery(options: ListCommentsOptions): string {
  const parts: string[] = [];

  if (options.limit !== undefined) {
    parts.push(`limit=${encodeURIComponent(String(options.limit))}`);
  }
  if (options.cursor !== undefined && options.cursor !== '') {
    parts.push(`cursor=${encodeURIComponent(options.cursor)}`);
  }

  return parts.length === 0 ? '' : `?${parts.join('&')}`;
}

/** The conversation endpoints. */
export const conversationsApi = {
  /**
   * POST /versions/{id}/comments — comments on a version as the authenticated
   * user. The author is taken from `token` by the server, never from the body.
   */
  createComment(
    versionId: string,
    token: string,
    payload: CreateCommentPayload,
  ): Promise<CommentResponse> {
    return request<CommentResponse>(`/versions/${encodeURIComponent(versionId)}/comments`, {
      method: 'POST',
      body: payload,
      token,
    });
  },

  /** GET /versions/{id}/comments — one page of a version's comments. Public. A
   * token hides comments by blocked authors. */
  listComments(
    versionId: string,
    options: ListCommentsOptions = {},
    token?: string,
  ): Promise<CommentListResponse> {
    return request<CommentListResponse>(
      `/versions/${encodeURIComponent(versionId)}/comments${threadQuery(options)}`,
      { method: 'GET', token },
    );
  },

  /**
   * GET /comments/{id} — resolves one comment, naming its version and story. It
   * is how a notification that points at a comment finds the thread to open.
   * Public. A token hides a blocked author's comment (the server answers 404).
   */
  getComment(id: string, token?: string): Promise<CommentDetailResponse> {
    return request<CommentDetailResponse>(`/comments/${encodeURIComponent(id)}`, {
      method: 'GET',
      token,
    });
  },
};

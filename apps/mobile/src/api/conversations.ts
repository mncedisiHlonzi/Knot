/**
 * The conversation endpoints of the Knot HTTP API: comments on a story version,
 * and the bridges that connect a comment in one language to a comment in
 * another.
 *
 * Everything here goes through the shared transport in `client.ts`, so error
 * handling is identical to the auth, story, and version endpoints: a failure is
 * always an `ApiError` carrying the server's machine-readable code.
 */
import { request } from './client';

/** One comment on a story version. */
export type Comment = {
  readonly id: string;
  readonly version_id: string;
  readonly author_id: string;
  readonly language: string;
  readonly body: string;
  readonly created_at: string;
  readonly updated_at: string;
};

/** The request body for POST /versions/{id}/comments. */
export type CreateCommentPayload = {
  readonly body: string;
  readonly language: string;
};

/** The body returned by POST /versions/{id}/comments. */
export type CommentResponse = {
  readonly comment: Comment;
};

/** The body returned by GET /versions/{id}/comments. */
export type CommentListResponse = {
  readonly comments: readonly Comment[];
  /** The token that resumes the thread after this page, or "" at the end. */
  readonly next_cursor: string;
};

/** A bridge: a comment in one language joined to a comment in another. */
export type Bridge = {
  readonly id: string;
  readonly source_comment_id: string;
  readonly target_comment_id: string;
  readonly author_id: string;
  readonly target_language: string;
  readonly adaptation_note: string | null;
  readonly created_at: string;
};

/** The request body for POST /comments/{id}/bridges. */
export type CreateBridgePayload = {
  readonly target_language: string;
  readonly body: string;
  readonly adaptation_note?: string;
};

/** The body returned by POST /comments/{id}/bridges. */
export type CreateBridgeResponse = {
  readonly bridge: Bridge;
  readonly source_comment: Comment;
  readonly target_comment: Comment;
};

/** The body returned by GET /comments/{id}/bridges. */
export type BridgeListResponse = {
  readonly bridges: readonly Bridge[];
};

/** The body returned by GET /bridges/{id}. */
export type BridgeResponse = {
  readonly bridge: Bridge;
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

  /** GET /versions/{id}/comments — one page of a version's comments. Public. */
  listComments(versionId: string, options: ListCommentsOptions = {}): Promise<CommentListResponse> {
    return request<CommentListResponse>(
      `/versions/${encodeURIComponent(versionId)}/comments${threadQuery(options)}`,
      { method: 'GET' },
    );
  },

  /**
   * POST /comments/{id}/bridges — bridges a comment into another language,
   * creating a new comment there. The bridger is taken from `token`.
   */
  createBridge(
    commentId: string,
    token: string,
    payload: CreateBridgePayload,
  ): Promise<CreateBridgeResponse> {
    return request<CreateBridgeResponse>(`/comments/${encodeURIComponent(commentId)}/bridges`, {
      method: 'POST',
      body: payload,
      token,
    });
  },

  /** GET /comments/{id}/bridges — every bridge touching a comment. Public. */
  listBridgesForComment(commentId: string): Promise<BridgeListResponse> {
    return request<BridgeListResponse>(`/comments/${encodeURIComponent(commentId)}/bridges`, {
      method: 'GET',
    });
  },

  /** GET /bridges/{id} — reads one bridge. Public. */
  getBridge(id: string): Promise<BridgeResponse> {
    return request<BridgeResponse>(`/bridges/${encodeURIComponent(id)}`, { method: 'GET' });
  },
};

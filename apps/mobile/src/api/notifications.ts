/**
 * The notification endpoints of the Knot HTTP API: the signed-in user's in-app
 * inbox.
 *
 * Everything here goes through the shared transport in `client.ts`, so error
 * handling is identical to the auth, story, and conversation endpoints: a failure
 * is always an `ApiError` carrying the server's machine-readable code.
 *
 * Every route is scoped to the caller by the server, so there is no user id in any
 * of these calls: the token names the inbox.
 */
import { request } from './client';
import type { AuthorRooted } from './rooted';

/**
 * What happened, as the server names it. The set is closed: the server's schema
 * refuses anything else.
 */
export type NotificationEventType = 'version.created' | 'comment.created' | 'bridge.created';

/** The kind of thing `entity_id` names. */
export type NotificationEntityType = 'story' | 'version' | 'comment' | 'bridge';

/** The user who acted, as the inbox renders them. */
export type NotificationActor = {
  readonly id: string;
  readonly display_name: string;
  /** A path on the API (`/users/{id}/avatar?v=…`), or "" when they have no avatar. */
  readonly avatar_url: string;
  /** The actor's primary public Rooted signal, or null when they have none. */
  readonly author_rooted?: AuthorRooted | null;
};

/** One notification in the inbox. */
export type Notification = {
  readonly id: string;
  readonly event_type: NotificationEventType;
  readonly entity_type: NotificationEntityType;
  /** The id of the thing to open: a version, a comment, or a bridge. */
  readonly entity_id: string;
  readonly read: boolean;
  readonly created_at: string;
  /** The user who acted, or null when their account can no longer be resolved. */
  readonly actor: NotificationActor | null;
};

/** The body returned by GET /notifications. */
export type NotificationListResponse = {
  readonly notifications: readonly Notification[];
  /** The token that resumes the inbox after this page, or "" at the end. */
  readonly next_cursor: string;
};

/** The body returned by GET /notifications/unread_count. */
export type UnreadCountResponse = {
  readonly count: number;
};

/** The body returned by POST /notifications/read_all. */
export type MarkAllReadResponse = {
  /** How many notifications were actually marked read. */
  readonly updated: number;
};

/** Options for an inbox page. */
export type ListNotificationsOptions = {
  /** A cursor from a previous page. Omit it to start at the newest notification. */
  readonly cursor?: string;
  /** How many to ask for. The server defaults to 20 and caps at 50. */
  readonly limit?: number;
};

/** The page size the inbox asks for. */
export const NOTIFICATIONS_PAGE_SIZE = 20;

/**
 * Builds the query string for an inbox request, omitting empty values so the
 * server applies its own defaults.
 */
function inboxQuery(options: ListNotificationsOptions): string {
  const parts: string[] = [];

  if (options.limit !== undefined) {
    parts.push(`limit=${encodeURIComponent(String(options.limit))}`);
  }
  if (options.cursor !== undefined && options.cursor !== '') {
    parts.push(`cursor=${encodeURIComponent(options.cursor)}`);
  }

  return parts.length === 0 ? '' : `?${parts.join('&')}`;
}

/** The notification endpoints. Every one of them requires the access token. */
export const notificationsApi = {
  /** GET /notifications — one page of the signed-in user's inbox, newest first. */
  listNotifications(
    token: string,
    options: ListNotificationsOptions = {},
  ): Promise<NotificationListResponse> {
    return request<NotificationListResponse>(`/notifications${inboxQuery(options)}`, {
      method: 'GET',
      token,
    });
  },

  /** GET /notifications/unread_count — the number for the feed's bell badge. */
  getUnreadCount(token: string): Promise<UnreadCountResponse> {
    return request<UnreadCountResponse>('/notifications/unread_count', {
      method: 'GET',
      token,
    });
  },

  /**
   * POST /notifications/{id}/read — marks one notification read.
   *
   * The server answers 204 with no body, which the transport surfaces as
   * `undefined`.
   */
  markRead(id: string, token: string): Promise<void> {
    return request<void>(`/notifications/${encodeURIComponent(id)}/read`, {
      method: 'POST',
      token,
    });
  },

  /** POST /notifications/read_all — marks every unread notification read. */
  markAllRead(token: string): Promise<MarkAllReadResponse> {
    return request<MarkAllReadResponse>('/notifications/read_all', {
      method: 'POST',
      token,
    });
  },
};

/**
 * The profile endpoints of the Knot HTTP API: a user's public wall.
 *
 * Everything here goes through the shared transport in `client.ts`, so error
 * handling is identical to the other endpoints: a failure is always an `ApiError`
 * carrying the server's machine-readable code.
 *
 * The wall is public, so no token is needed. It is one chronological stream
 * merged from the things a person can author — stories, versions (adaptations),
 * comments, bridges, inquiries, and answers to inquiries.
 */
import { request } from './client';
import type { AuthorRooted } from './rooted';

/** What a user did, as the server names it. The set is closed. */
export type ActivityKind =
  'story' | 'version' | 'comment' | 'bridge' | 'inquiry' | 'inquiry_answer';

/** The context of a story activity. */
export type StoryActivityPayload = {
  readonly title: string;
  readonly pillar: string;
  readonly language: string;
};

/** The context of an adaptation activity. */
export type VersionActivityPayload = {
  readonly story_id: string;
  readonly story_title: string;
  readonly language: string;
};

/** The context of a comment activity. */
export type CommentActivityPayload = {
  readonly version_id: string;
  readonly story_id: string;
  readonly body_preview: string;
};

/** The context of a bridge activity. */
export type BridgeActivityPayload = {
  readonly source_comment_id: string;
  readonly version_id: string;
  readonly target_language: string;
};

/** The context of an inquiry activity: the question the owner asked. */
export type InquiryActivityPayload = {
  readonly title: string;
  /** The place asked about, or absent when the question names none. */
  readonly place?: string;
};

/** The context of an inquiry_answer activity: the question the owner answered. */
export type InquiryAnswerActivityPayload = {
  readonly inquiry_id: string;
  readonly inquiry_title: string;
  readonly body_preview: string;
};

/**
 * One entry on a wall. A discriminated union on `kind`, so narrowing on `kind`
 * narrows `payload` to exactly the fields that kind carries.
 *
 * Every activity is authored by the wall's owner, so the author is the `user`
 * header; there is no per-activity author.
 */
export type Activity =
  | {
      readonly kind: 'story';
      readonly id: string;
      readonly created_at: string;
      readonly payload: StoryActivityPayload;
    }
  | {
      readonly kind: 'version';
      readonly id: string;
      readonly created_at: string;
      readonly payload: VersionActivityPayload;
    }
  | {
      readonly kind: 'comment';
      readonly id: string;
      readonly created_at: string;
      readonly payload: CommentActivityPayload;
    }
  | {
      readonly kind: 'bridge';
      readonly id: string;
      readonly created_at: string;
      readonly payload: BridgeActivityPayload;
    }
  | {
      readonly kind: 'inquiry';
      readonly id: string;
      readonly created_at: string;
      readonly payload: InquiryActivityPayload;
    }
  | {
      readonly kind: 'inquiry_answer';
      readonly id: string;
      readonly created_at: string;
      readonly payload: InquiryAnswerActivityPayload;
    };

/** The public identity header of a wall. */
export type ProfileUser = {
  readonly id: string;
  readonly display_name: string;
  /** A path on the API, or null when the user has no avatar. */
  readonly avatar_url: string | null;
  /** The owner's primary public Rooted summary, or null when they have none. */
  readonly rooted: AuthorRooted | null;
  /** When the account was created, as an ISO-8601 timestamp. */
  readonly joined_at: string;
};

/** The body returned by GET /users/{id}/profile. */
export type UserProfile = {
  readonly user: ProfileUser;
  readonly activities: readonly Activity[];
  /** The token that resumes the wall after this page, or "" at the end. */
  readonly next_cursor: string;
};

/** The page size the wall asks for. */
export const PROFILE_PAGE_SIZE = 20;

/** The profile endpoints. */
export const profileApi = {
  /**
   * GET /users/{id}/profile — one page of a user's public wall, newest first.
   *
   * `cursor` omits the first page; `limit` defaults to the server's 20 and is
   * clamped to 50. Both are omitted from the query string when not needed.
   */
  getUserProfile(userId: string, cursor?: string, limit?: number): Promise<UserProfile> {
    const parts: string[] = [];

    if (limit !== undefined) {
      parts.push(`limit=${encodeURIComponent(String(limit))}`);
    }
    if (cursor !== undefined && cursor !== '') {
      parts.push(`cursor=${encodeURIComponent(cursor)}`);
    }

    const query = parts.length === 0 ? '' : `?${parts.join('&')}`;

    return request<UserProfile>(`/users/${encodeURIComponent(userId)}/profile${query}`, {
      method: 'GET',
    });
  },
};

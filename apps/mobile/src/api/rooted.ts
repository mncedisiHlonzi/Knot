/**
 * The Rooted endpoints of the Knot HTTP API.
 *
 * Rooted is a person's self-declared connection to a place: a place name at city
 * or region precision, and a duration bucket. A signal is honest (self-declared,
 * never verified) and privacy-conscious (place only, never a precise location).
 *
 * Everything here goes through the shared transport in `client.ts`, so error
 * handling is identical to the auth, story, version, and conversation endpoints:
 * a failure is always an `ApiError` carrying the server's machine-readable code.
 */
import { request } from './client';

/** The duration buckets the server accepts. */
export type DurationBucket =
  'lifelong' | 'many_years' | 'several_years' | 'a_few_years' | 'recently';

/** The duration buckets, in the order the UI offers them. */
export const DURATION_BUCKETS: readonly DurationBucket[] = [
  'lifelong',
  'many_years',
  'several_years',
  'a_few_years',
  'recently',
];

/**
 * The inline Rooted summary the API attaches to content responses.
 *
 * It carries only what an inline badge needs: no id, no user id, and no
 * timestamps. The server sends `null` when the author has no public signal.
 */
export type AuthorRooted = {
  readonly place: string;
  readonly duration_bucket: DurationBucket;
};

/** A stored Rooted signal. */
export type RootedSignal = {
  readonly id: string;
  readonly user_id: string;
  readonly place: string;
  /** Structured place data, or null when the owner attached no coordinate. */
  readonly latitude: number | null;
  readonly longitude: number | null;
  readonly place_country: string | null;
  readonly duration_bucket: DurationBucket;
  readonly is_public: boolean;
  readonly is_primary: boolean;
  readonly created_at: string;
  readonly updated_at: string;
};

/** The request body for POST /users/me/rooted. */
export type SetSignalPayload = {
  readonly place: string;
  /** Structured place data from the location picker; the pair goes together. */
  readonly latitude?: number;
  readonly longitude?: number;
  readonly place_country?: string;
  readonly duration_bucket: DurationBucket;
  /** Omit to keep the default, which is public. */
  readonly is_public?: boolean;
};

/** The body returned by POST /users/me/rooted. */
export type RootedSignalResponse = {
  readonly signal: RootedSignal;
};

/** The body returned by GET /users/me/rooted and GET /users/{id}/rooted. */
export type RootedSignalListResponse = {
  readonly signals: readonly RootedSignal[];
};

/** The Rooted endpoints. */
export const rootedApi = {
  /**
   * POST /users/me/rooted — sets the authenticated user's primary Rooted signal.
   *
   * A second call replaces the first: a user has exactly one active signal. The
   * owner is taken from `token` by the server; it is never sent in the body.
   */
  setMySignal(token: string, payload: SetSignalPayload): Promise<RootedSignalResponse> {
    return request<RootedSignalResponse>('/users/me/rooted', {
      method: 'POST',
      body: payload,
      token,
    });
  },

  /**
   * GET /users/me/rooted — the authenticated user's own signals, including any
   * that are hidden from public read.
   */
  getMySignals(token: string): Promise<RootedSignalListResponse> {
    return request<RootedSignalListResponse>('/users/me/rooted', { method: 'GET', token });
  },

  /** GET /users/{id}/rooted — another user's public signals. Public endpoint. */
  getUserSignals(userId: string): Promise<RootedSignalListResponse> {
    return request<RootedSignalListResponse>(`/users/${encodeURIComponent(userId)}/rooted`, {
      method: 'GET',
    });
  },
};

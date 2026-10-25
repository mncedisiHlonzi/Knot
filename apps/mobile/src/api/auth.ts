/**
 * The auth endpoints of the Knot HTTP API.
 *
 * Nine calls, one per endpoint, all going through the shared transport in
 * `client.ts` so an error is always an `ApiError` carrying the server's
 * machine-readable code, and a `401` is retried once after a refresh.
 *
 * The shapes here mirror the Go handlers exactly; see `docs/API.md`.
 */
import { request } from './client';
import type { AuthResponse, User } from './client';

/** The request body for `POST /auth/register`. */
export type RegisterPayload = {
  readonly email: string;
  readonly password: string;
  readonly display_name: string;
  readonly preferred_languages?: readonly string[];
  readonly approximate_location?: string;
  readonly phone?: string;
};

/**
 * The `POST /auth/register` body.
 *
 * It carries the account and **no tokens**: an account cannot sign in until its
 * address is verified (KNOT-ADR-083), so registration is not a way in.
 */
export type RegisterResult = {
  readonly user: User;
};

/** The body of every endpoint whose only outcome is "done". */
export type SuccessResult = {
  readonly success: boolean;
};

/** One signed-in device, as `GET /auth/sessions` reports it. */
export type SessionSummary = {
  readonly id: string;
  /** Whatever the client said about itself at sign-in. `{}` when it said nothing. */
  readonly device_info: Readonly<Record<string, unknown>>;
  /** The address the session was created from, or "" when unknown. */
  readonly ip_address: string;
  readonly last_used_at: string;
  readonly created_at: string;
  /** True for the session this request was made from. */
  readonly current: boolean;
};

/** The `GET /auth/sessions` body. */
export type SessionList = {
  readonly sessions: readonly SessionSummary[];
};

/**
 * Creates an account.
 *
 * A verification link is written to the server console (KNOT-ADR-088); the caller
 * shows the "check your email" state and must not treat this as being signed in.
 */
export function register(payload: RegisterPayload): Promise<RegisterResult> {
  return request<RegisterResult>('/auth/register', { method: 'POST', body: payload });
}

/**
 * Redeems a verification token, which unlocks sign-in for the account.
 *
 * The failure is `invalid_verification_token` for an unknown, expired, or
 * already-used token; the screen says so rather than distinguishing them, because
 * the remedy for all three is the same: request a new link.
 */
export function verifyEmail(token: string): Promise<SuccessResult> {
  return request<SuccessResult>('/auth/verify-email', {
    method: 'POST',
    body: { token },
  });
}

/**
 * Exchanges credentials for a session.
 *
 * `deviceInfo` is free-form detail stored with the session and shown back in the
 * device list; it is never used for an authorisation decision.
 *
 * An unverified account is refused with `403 email_not_verified`, which the Login
 * screen recognises so it can explain what to do.
 */
export function login(
  email: string,
  password: string,
  deviceInfo?: Readonly<Record<string, unknown>>,
): Promise<AuthResponse> {
  return request<AuthResponse>('/auth/login', {
    method: 'POST',
    body: {
      email,
      password,
      ...(deviceInfo === undefined ? {} : { device_info: deviceInfo }),
    },
  });
}

/**
 * Rotates the refresh token and mints a new pair.
 *
 * Exported because `loadSession` spends the stored token on launch. In-app
 * refreshes go through `fetchWithRefresh` instead, which shares one refresh across
 * concurrent failures.
 */
export function refresh(refreshToken: string): Promise<AuthResponse> {
  return request<AuthResponse>('/auth/refresh', {
    method: 'POST',
    body: { refresh_token: refreshToken },
  });
}

/**
 * Revokes the session the access token belongs to.
 *
 * The server reads the session from the token's own `sid` claim, so no id is sent:
 * a caller cannot ask to end somebody else's device.
 */
export function logout(accessToken: string): Promise<SuccessResult> {
  return request<SuccessResult>('/auth/logout', { method: 'POST', token: accessToken });
}

/** Lists the caller's live sessions, newest first. */
export function listSessions(accessToken: string): Promise<SessionList> {
  return request<SessionList>('/auth/sessions', { method: 'GET', token: accessToken });
}

/** Revokes one of the caller's own sessions. */
export function revokeSession(id: string, accessToken: string): Promise<SuccessResult> {
  return request<SuccessResult>(`/auth/sessions/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    token: accessToken,
  });
}

/**
 * Starts a password reset.
 *
 * It succeeds whether or not the address has an account — the endpoint answers the
 * same way on purpose (KNOT-ADR-087) — so the screen's message must not imply the
 * address was found.
 */
export function forgotPassword(email: string): Promise<SuccessResult> {
  return request<SuccessResult>('/auth/forgot-password', {
    method: 'POST',
    body: { email },
  });
}

/**
 * Sets a new password from a reset token, signing every session out.
 *
 * The new password must satisfy the same policy as registration; the server is the
 * authority and answers `400 validation_error`.
 */
export function resetPassword(token: string, newPassword: string): Promise<SuccessResult> {
  return request<SuccessResult>('/auth/reset-password', {
    method: 'POST',
    body: { token, new_password: newPassword },
  });
}

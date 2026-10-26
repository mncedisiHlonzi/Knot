/**
 * A small typed wrapper around `fetch` for the Knot HTTP API.
 *
 * It exists so screens never deal with status codes, JSON parsing, or the server's
 * error envelope directly. Responses are typed, and every failure is raised as an
 * ApiError carrying the machine-readable `code` the backend returned.
 *
 * It also owns the access-token lifecycle: an authenticated request that comes back
 * `401` is retried **once** after refreshing, and concurrent failures share a single
 * refresh (see `fetchWithRefresh`).
 */
import { API_BASE_URL } from '../config/api';

/** A user as returned by the API. Never includes a password or hash. */
export type User = {
  readonly id: string;
  readonly email: string;
  readonly display_name: string;
  readonly preferred_languages: readonly string[];
  readonly approximate_location: string;
  readonly phone: string;
  readonly created_at: string;
  /**
   * When the address was proven, or null while it has not been (KNOT-ADR-083).
   * An account cannot sign in until this is set, so the client uses it to explain a
   * `403 email_not_verified` instead of guessing.
   */
  readonly email_verified_at: string | null;
  /**
   * The path on the API that serves the user's avatar, or an empty string when
   * they have none. It is a relative path (`/users/{id}/avatar?v=…`), never a
   * link to object storage; resolve it against the API base URL to render it.
   */
  readonly avatar_url: string;
};

/** The body returned by `POST /auth/login` and `POST /auth/refresh`. */
export type AuthResponse = {
  readonly user: User;
  readonly access_token: string;
  readonly refresh_token: string;
  readonly expires_in: number;
  /**
   * True when the sign-in cancelled a pending account deletion, so the app can welcome
   * the person back instead of pretending nothing happened (KNOT-ADR-081). Absent on a
   * refresh, which never recovers anything.
   */
  readonly recovered?: boolean;
};

/** The body of GET /health. */
export type HealthResponse = {
  readonly status: string;
  readonly version: string;
  readonly time: string;
};

/** Error code used when the request never reached the server. */
export const NETWORK_ERROR_CODE = 'network_error';

/** The error code the API uses when an access token is missing or no longer valid. */
export const UNAUTHORIZED_CODE = 'unauthorized';

/**
 * An error raised by the API client. It carries the HTTP status (0 when the
 * request never completed) and the server's machine-readable error code.
 */
export class ApiError extends Error {
  readonly status: number;

  readonly code: string;

  constructor(message: string, status: number, code: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

/** The server's error envelope. */
type ErrorEnvelope = {
  readonly error?: {
    readonly code?: string;
    readonly message?: string;
  };
};

/** The minimal request description this client builds. */
type RequestOptions = {
  readonly method: 'GET' | 'POST' | 'DELETE';
  readonly body?: unknown;
  /**
   * Access token for a protected endpoint, sent as a bearer credential. Public
   * endpoints omit it so no credential is ever attached to a request that does
   * not need one.
   */
  readonly token?: string;
};

/**
 * What the client needs from the session module to be able to refresh.
 *
 * It is injected rather than imported so this file never depends on storage: the
 * session module owns where a refresh token lives, and the client only asks for one.
 */
export type SessionBridge = {
  /** The refresh token to spend, or null when there is no session. */
  getRefreshToken: () => string | null;
  /** Called with the freshly rotated pair after a successful refresh. */
  onTokensRefreshed: (result: AuthResponse) => void;
  /** Called when the server refused the refresh token outright. */
  onSessionExpired: () => void;
};

let sessionBridge: SessionBridge | null = null;

/**
 * Registers the session bridge. The app calls this once on start-up.
 *
 * Passing null detaches it, which is what sign-out does: after that, no request can
 * silently refresh a session that no longer exists.
 */
export function configureSessionBridge(bridge: SessionBridge | null): void {
  sessionBridge = bridge;
}

/**
 * The refresh currently in flight, if any.
 *
 * Concurrent 401s must not each spend the refresh token: rotation invalidates the
 * token it was given, so the second caller's refresh would fail and — because a
 * replayed token is treated as theft (KNOT-ADR-085) — sign the user out of every
 * device. Sharing one promise is therefore a correctness requirement, not an
 * optimisation.
 */
let refreshInFlight: Promise<string | null> | null = null;

/**
 * Refreshes the access token, or returns null when the session is over.
 *
 * Network failures and a missing bridge return null **without** clearing the
 * session: being offline is not the same as being signed out, and the caller's next
 * attempt may well succeed.
 */
export function refreshAccessToken(): Promise<string | null> {
  if (refreshInFlight === null) {
    refreshInFlight = performRefresh().finally(() => {
      refreshInFlight = null;
    });
  }

  return refreshInFlight;
}

/**
 * Performs one refresh exchange.
 *
 * It deliberately does not go through `request`, so a 401 from the refresh endpoint
 * cannot recurse into another refresh.
 */
async function performRefresh(): Promise<string | null> {
  const bridge = sessionBridge;
  const refreshToken = bridge === null ? null : bridge.getRefreshToken();
  if (bridge === null || refreshToken === null || refreshToken === '') {
    return null;
  }

  let response: Response;
  try {
    response = await fetch(`${API_BASE_URL}/auth/refresh`, {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
    });
  } catch {
    return null;
  }

  if (response.status === 401 || response.status === 400) {
    bridge.onSessionExpired();
    return null;
  }
  if (!response.ok) {
    return null;
  }

  let body: AuthResponse;
  try {
    body = (await response.json()) as AuthResponse;
  } catch {
    return null;
  }

  bridge.onTokensRefreshed(body);

  return body.access_token;
}

/**
 * Sends one request and returns the raw response.
 */
async function send(path: string, options: RequestOptions): Promise<Response> {
  const url = `${API_BASE_URL}${path}`;

  return fetch(url, {
    method: options.method,
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
      ...(options.token === undefined ? {} : { Authorization: `Bearer ${options.token}` }),
    },
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  });
}

/**
 * Sends an authenticated request, refreshing the access token once if it expired.
 *
 * The rules, in order:
 *
 *  1. A `401` on a request that carried a token triggers exactly one refresh.
 *  2. The original request is retried **once** with the new token — never looped.
 *  3. If the refresh fails outright the session is over; if it fails for a network
 *     reason the original 401 is returned and the session is left alone.
 *  4. Requests without a token are never retried: a public endpoint answering 401
 *     is not something a refresh can fix.
 *
 * It is exported so the behaviour can be tested without a React tree.
 */
export async function fetchWithRefresh(path: string, options: RequestOptions): Promise<Response> {
  let response: Response;
  try {
    response = await send(path, options);
  } catch (error) {
    const message = error instanceof Error ? error.message : 'unknown network failure';
    throw new ApiError(`could not reach the Knot API: ${message}`, 0, NETWORK_ERROR_CODE);
  }

  if (response.status !== 401 || options.token === undefined) {
    return response;
  }

  const refreshedToken = await refreshAccessToken();
  if (refreshedToken === null) {
    return response;
  }

  try {
    return await send(path, { ...options, token: refreshedToken });
  } catch (error) {
    const message = error instanceof Error ? error.message : 'unknown network failure';
    throw new ApiError(`could not reach the Knot API: ${message}`, 0, NETWORK_ERROR_CODE);
  }
}

/**
 * Performs a request and returns the parsed JSON body, or throws an ApiError.
 *
 * It is exported so that the endpoint modules (`auth.ts`, `stories.ts`) share one
 * transport, and therefore one error envelope, one refresh policy and one
 * network-failure message.
 */
export async function request<T>(path: string, options: RequestOptions): Promise<T> {
  const response = await fetchWithRefresh(path, options);

  const text = await response.text();

  if (!response.ok) {
    throw toApiError(response.status, text);
  }

  if (text === '') {
    return undefined as T;
  }

  try {
    return JSON.parse(text) as T;
  } catch {
    throw new ApiError(
      'the Knot API returned a malformed response',
      response.status,
      'invalid_response',
    );
  }
}

/**
 * Turns a non-2xx response into an ApiError, preferring the server's own message.
 */
function toApiError(status: number, body: string): ApiError {
  let code = 'unknown_error';
  let message = `request failed with status ${status}`;

  try {
    const parsed = JSON.parse(body) as ErrorEnvelope;
    if (typeof parsed.error?.code === 'string') {
      code = parsed.error.code;
    }
    if (typeof parsed.error?.message === 'string' && parsed.error.message !== '') {
      message = parsed.error.message;
    }
  } catch {
    // A non-JSON error body is unusual but not worth failing over; the status
    // based defaults above are already useful.
  }

  return new ApiError(message, status, code);
}

/**
 * Renders an unknown thrown value as a message safe to show to a user.
 */
export function describeError(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message;
  }
  if (error instanceof Error) {
    return error.message;
  }
  return 'something went wrong';
}

/**
 * Reports whether an error is the API refusing an unverified account, so screens can
 * offer the verification path instead of showing a bare message.
 */
export function isEmailNotVerified(error: unknown): boolean {
  return error instanceof ApiError && error.code === 'email_not_verified';
}

/** A file to upload: the local URI the OS handed the picker, a filename, and its
 * MIME type. React Native's FormData accepts this object in place of a Blob.
 */
export type UploadFilePart = {
  readonly uri: string;
  readonly name: string;
  readonly type: string;
};

/** Options for a multipart upload. */
export type UploadOptions = {
  readonly file: UploadFilePart;
  /** Extra non-file fields, sent as multipart form fields. */
  readonly fields?: Readonly<Record<string, string>>;
  /** Access token for a protected endpoint. */
  readonly token?: string;
};

/**
 * Performs a multipart POST and returns the parsed JSON body, or throws an
 * ApiError.
 *
 * It shares the error envelope with `request`; it exists separately because a
 * multipart body must not declare a Content-Type — the runtime sets it, boundary
 * and all. It refreshes on a 401 the same way, by re-sending the whole multipart
 * body once with the new token.
 */
export async function uploadFile<T>(path: string, options: UploadOptions): Promise<T> {
  const response = await fetchWithRefreshMultipart(path, options);

  const text = await response.text();

  if (!response.ok) {
    throw toApiError(response.status, text);
  }

  if (text === '') {
    return undefined as T;
  }

  try {
    return JSON.parse(text) as T;
  } catch {
    throw new ApiError(
      'the Knot API returned a malformed response',
      response.status,
      'invalid_response',
    );
  }
}

/** Sends one multipart request. */
async function sendMultipart(path: string, options: UploadOptions): Promise<Response> {
  const form = new FormData();
  // React Native's FormData accepts {uri, name, type} where the DOM types expect
  // a Blob; the cast is the documented React Native idiom.
  form.append('file', options.file as unknown as Blob);
  for (const [key, value] of Object.entries(options.fields ?? {})) {
    form.append(key, value);
  }

  return fetch(`${API_BASE_URL}${path}`, {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      ...(options.token === undefined ? {} : { Authorization: `Bearer ${options.token}` }),
    },
    body: form,
  });
}

/** Sends a multipart request, retrying once after a refresh on 401. */
async function fetchWithRefreshMultipart(path: string, options: UploadOptions): Promise<Response> {
  let response: Response;
  try {
    response = await sendMultipart(path, options);
  } catch (error) {
    const message = error instanceof Error ? error.message : 'unknown network failure';
    throw new ApiError(`could not reach the Knot API: ${message}`, 0, NETWORK_ERROR_CODE);
  }

  if (response.status !== 401 || options.token === undefined) {
    return response;
  }

  const refreshedToken = await refreshAccessToken();
  if (refreshedToken === null) {
    return response;
  }

  return sendMultipart(path, { ...options, token: refreshedToken });
}

/** The public endpoints that need no credential. */
export const api = {
  /** GET /health */
  health(): Promise<HealthResponse> {
    return request<HealthResponse>('/health', { method: 'GET' });
  },
};

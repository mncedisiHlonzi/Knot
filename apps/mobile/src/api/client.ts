/**
 * A small typed wrapper around `fetch` for the Knot HTTP API.
 *
 * It exists so screens never deal with status codes, JSON parsing, or the server's
 * error envelope directly. Responses are typed, and every failure is raised as an
 * ApiError carrying the machine-readable `code` the backend returned.
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
};

/** The body returned by both register and login. */
export type AuthResponse = {
  readonly user: User;
  readonly access_token: string;
  readonly refresh_token: string;
  readonly expires_in: number;
};

/** The body of GET /health. */
export type HealthResponse = {
  readonly status: string;
  readonly version: string;
  readonly time: string;
};

/** The request body for POST /auth/register. */
export type RegisterPayload = {
  readonly email: string;
  readonly password: string;
  readonly display_name: string;
  readonly preferred_languages?: readonly string[];
  readonly approximate_location?: string;
  readonly phone?: string;
};

/** The request body for POST /auth/login. */
export type LoginPayload = {
  readonly email: string;
  readonly password: string;
};

/** Error code used when the request never reached the server. */
export const NETWORK_ERROR_CODE = 'network_error';

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
  readonly method: 'GET' | 'POST';
  readonly body?: unknown;
};

/**
 * Performs a request and returns the parsed JSON body, or throws an ApiError.
 */
async function request<T>(path: string, options: RequestOptions): Promise<T> {
  const url = `${API_BASE_URL}${path}`;

  let response: Response;
  try {
    response = await fetch(url, {
      method: options.method,
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : 'unknown network failure';
    throw new ApiError(`could not reach the Knot API: ${message}`, 0, NETWORK_ERROR_CODE);
  }

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

/** The Knot API endpoints this app currently uses. */
export const api = {
  /** GET /health */
  health(): Promise<HealthResponse> {
    return request<HealthResponse>('/health', { method: 'GET' });
  },

  /** POST /auth/register */
  register(payload: RegisterPayload): Promise<AuthResponse> {
    return request<AuthResponse>('/auth/register', { method: 'POST', body: payload });
  },

  /** POST /auth/login */
  login(payload: LoginPayload): Promise<AuthResponse> {
    return request<AuthResponse>('/auth/login', { method: 'POST', body: payload });
  },
};

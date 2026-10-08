/**
 * API configuration for the Knot mobile app.
 *
 * React Native has no populated `process.env` at runtime, and this scaffold
 * deliberately adds no build-time env-inlining plugin (KNOT-003 adds no npm
 * dependencies). The base URL therefore comes from a constant — `DEV_API_URL`
 * in `./dev.ts`, the one place to point the app at a different backend — with a
 * defensive read of `process.env.KNOT_API_URL` so that a future build which does
 * inline it takes precedence without changing this module.
 */
import { DEV_API_URL } from './dev';

/** Used when no build-time override is present. */
const DEFAULT_API_BASE_URL = DEV_API_URL;

/**
 * Matches `scheme://host[:port][/path]` with no whitespace. A hand-rolled check is
 * used instead of the global `URL` constructor because this project's tsconfig
 * does not include the DOM lib.
 */
const API_BASE_URL_PATTERN = /^https?:\/\/[A-Za-z0-9.-]+(?::\d{1,5})?(?:\/\S*)?$/;

/** The slice of `process.env` this module reads. */
type BuildTimeEnv = {
  readonly KNOT_API_URL?: string;
};

/**
 * Reports whether a string is a usable HTTP(S) API base URL.
 */
export function isValidApiBaseUrl(value: string): boolean {
  return API_BASE_URL_PATTERN.test(value);
}

/**
 * Reads `process.env.KNOT_API_URL` if the runtime exposes it.
 *
 * The cast through `globalThis` avoids depending on Node type definitions while
 * the optional chaining keeps the app working when `process` is entirely absent.
 */
function readBuildTimeApiUrl(): string | undefined {
  const runtime = (globalThis as { process?: { env?: BuildTimeEnv } }).process;
  const value = runtime?.env?.KNOT_API_URL;

  if (typeof value !== 'string' || value.trim() === '') {
    return undefined;
  }

  return value.trim();
}

/**
 * Normalises a candidate URL by removing any trailing slashes.
 */
function stripTrailingSlashes(value: string): string {
  return value.replace(/\/+$/, '');
}

/**
 * The base URL of the Knot API, without a trailing slash.
 *
 * Throws at module load if the configured value is not a valid HTTP(S) URL, so a
 * misconfiguration surfaces immediately rather than as a confusing request error.
 */
export const API_BASE_URL: string = ((): string => {
  const candidate = stripTrailingSlashes(readBuildTimeApiUrl() ?? DEFAULT_API_BASE_URL);

  if (!isValidApiBaseUrl(candidate)) {
    throw new Error(`Invalid Knot API base URL: ${candidate}`);
  }

  return candidate;
})();

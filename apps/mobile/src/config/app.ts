/**
 * Static application metadata.
 *
 * Placeholder values only — no product configuration or domain logic lives here.
 */
export const APP_NAME = 'Knot';
export const APP_VERSION = '0.1.0';
export const APP_TAGLINE = 'Tying human stories, languages, and truths together.';

export type AppMetadata = {
  readonly name: string;
  readonly version: string;
  readonly tagline: string;
};

export const appMetadata: AppMetadata = {
  name: APP_NAME,
  tagline: APP_TAGLINE,
  version: APP_VERSION,
};

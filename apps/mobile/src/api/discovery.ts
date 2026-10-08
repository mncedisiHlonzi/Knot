/**
 * The discovery endpoints of the Knot HTTP API: finding stories by place.
 *
 * Discovery groups stories into place clusters — one entry per place, with how
 * many stories are there, which pillars they belong to, which languages they are
 * told in, and when the newest one was published — and lists the stories at a
 * single place. Places are free text: the server stores no coordinates, so the
 * client resolves a place name to a point visually (see `../data/placeCoordinates`).
 *
 * Everything here goes through the shared transport in `client.ts`, so error
 * handling is identical to every other endpoint: a failure is always an `ApiError`
 * carrying the server's machine-readable code.
 */
import { request } from './client';
import type { Pillar, StoryListResponse } from './stories';

/** The page size the place screen asks for. */
export const PLACE_PAGE_SIZE = 20;

/**
 * A place with the stories told there, as returned by `GET /discovery/clusters`.
 *
 * `place` is the author's original spelling; grouping is by the normalised lower
 * case form. `pillar_counts` is keyed by pillar and always carries both pillars.
 */
export type PlaceCluster = {
  readonly place: string;
  readonly story_count: number;
  readonly pillar_counts: Readonly<Record<string, number>>;
  readonly languages: readonly string[];
  readonly latest_story_at: string;
};

/** The body returned by `GET /discovery/clusters`. */
export type ClusterListResponse = {
  readonly clusters: readonly PlaceCluster[];
};

/** Options for a cluster request. Omitted values are not sent. */
export type ListClustersOptions = {
  readonly pillar?: Pillar;
  readonly language?: string;
  readonly limit?: number;
};

/** Options for a place request. */
export type ListPlaceStoriesOptions = {
  /** A cursor from a previous page. Omit it to start at the newest story. */
  readonly cursor?: string;
  /** How many stories to ask for. The server defaults to 20 and caps at 50. */
  readonly limit?: number;
};

/**
 * Builds the query string for a cluster request, omitting empty values so the
 * server applies its own defaults.
 */
function clusterQuery(options: ListClustersOptions): string {
  const parts: string[] = [];

  if (options.pillar !== undefined) {
    parts.push(`pillar=${encodeURIComponent(options.pillar)}`);
  }
  if (options.language !== undefined && options.language !== '') {
    parts.push(`language=${encodeURIComponent(options.language)}`);
  }
  if (options.limit !== undefined) {
    parts.push(`limit=${encodeURIComponent(String(options.limit))}`);
  }

  return parts.length === 0 ? '' : `?${parts.join('&')}`;
}

/**
 * Builds the query string for a place request, omitting empty values so the
 * server applies its own defaults.
 */
function placeQuery(options: ListPlaceStoriesOptions): string {
  const parts: string[] = [];

  if (options.limit !== undefined) {
    parts.push(`limit=${encodeURIComponent(String(options.limit))}`);
  }
  if (options.cursor !== undefined && options.cursor !== '') {
    parts.push(`cursor=${encodeURIComponent(options.cursor)}`);
  }

  return parts.length === 0 ? '' : `?${parts.join('&')}`;
}

/** The discovery endpoints. */
export const discoveryApi = {
  /** GET /discovery/clusters — places with stories, most stories first. Public. */
  listClusters(options: ListClustersOptions = {}): Promise<ClusterListResponse> {
    return request<ClusterListResponse>(`/discovery/clusters${clusterQuery(options)}`, {
      method: 'GET',
    });
  },

  /**
   * GET /discovery/places/{place} — one page of the stories at a place, newest
   * first. Public. The place is URL-encoded; the server matches it case
   * insensitively, so the caller's casing does not matter.
   */
  listStoriesAtPlace(
    place: string,
    options: ListPlaceStoriesOptions = {},
  ): Promise<StoryListResponse> {
    return request<StoryListResponse>(
      `/discovery/places/${encodeURIComponent(place)}${placeQuery(options)}`,
      { method: 'GET' },
    );
  },
};

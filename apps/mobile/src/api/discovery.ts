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
 * The `country` value that asks the server for the stories which record no
 * country. The server reserves the word "null" for exactly this purpose, so a
 * cluster whose `place_country` is null is opened with it (KNOT-020e-fix2).
 */
export const PLACE_COUNTRY_ABSENT = 'null';

/**
 * A place with the stories told there, as returned by `GET /discovery/clusters`.
 *
 * `place` is the author's original spelling; grouping is by the normalised lower
 * case form. `pillar_counts` is keyed by pillar and always carries both pillars.
 */
export type PlaceCluster = {
  readonly place: string;
  /**
   * The country the geocoder reported, or null for a legacy cluster that predates
   * structured place data.
   */
  readonly place_country: string | null;
  /**
   * The cluster's coordinate, or null for a legacy cluster. When present the map
   * plots it directly instead of consulting the local lookup table (KNOT-ADR-034).
   */
  readonly latitude: number | null;
  readonly longitude: number | null;
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
  /**
   * The country to narrow the page to, as a cluster reported it. `undefined` does
   * not filter — every story at the place name is returned, whatever country each
   * records. `null` asks for the stories that record no country (sent as the
   * reserved `PLACE_COUNTRY_ABSENT` token). A string matches exactly. This is what
   * keeps a country-scoped cluster's page to that cluster's own stories
   * (KNOT-020e-fix2).
   */
  readonly country?: string | null;
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
 *
 * A null country is a real filter, not an absent one, so it is sent as the
 * reserved absent token rather than dropped (KNOT-020e-fix2).
 */
export function placeQuery(options: ListPlaceStoriesOptions): string {
  const parts: string[] = [];

  if (options.limit !== undefined) {
    parts.push(`limit=${encodeURIComponent(String(options.limit))}`);
  }
  if (options.country === null) {
    parts.push(`country=${PLACE_COUNTRY_ABSENT}`);
  } else if (options.country !== undefined && options.country !== '') {
    parts.push(`country=${encodeURIComponent(options.country)}`);
  }
  if (options.cursor !== undefined && options.cursor !== '') {
    parts.push(`cursor=${encodeURIComponent(options.cursor)}`);
  }

  return parts.length === 0 ? '' : `?${parts.join('&')}`;
}

/** The discovery endpoints. */
export const discoveryApi = {
  /**
   * GET /discovery/clusters — places with stories, most stories first. Public.
   * A token hides blocked authors' stories: the server resolves the caller's
   * mutual block set, and a place left with no visible stories drops out of the
   * list entirely. Omit the token for the anonymous view.
   */
  listClusters(options: ListClustersOptions = {}, token?: string): Promise<ClusterListResponse> {
    return request<ClusterListResponse>(`/discovery/clusters${clusterQuery(options)}`, {
      method: 'GET',
      token,
    });
  },

  /**
   * GET /discovery/places/{place} — one page of the stories at a place, newest
   * first. Public. The place is URL-encoded; the server matches it case
   * insensitively, so the caller's casing does not matter. The optional `country`
   * narrows the page to one country (or, as `null`, to the stories that record
   * none), so a country-scoped cluster's page matches its story count. A token
   * hides blocked authors' stories; omit it for the anonymous view. Each story is
   * the same enriched projection the feed returns (KNOT-020e-fix2).
   */
  listStoriesAtPlace(
    place: string,
    options: ListPlaceStoriesOptions = {},
    token?: string,
  ): Promise<StoryListResponse> {
    return request<StoryListResponse>(
      `/discovery/places/${encodeURIComponent(place)}${placeQuery(options)}`,
      { method: 'GET', token },
    );
  },
};

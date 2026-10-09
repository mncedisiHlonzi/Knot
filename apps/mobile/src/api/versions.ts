/**
 * Tell My People: the story version endpoints of the Knot HTTP API.
 *
 * Everything here goes through the shared transport in `client.ts`, so error
 * handling is identical to the auth and story endpoints: a failure is always an
 * `ApiError` carrying the server's machine-readable code.
 *
 * A story's versions form a Language Tree expressed as an adjacency list: every
 * version names its parent, and the root version names none. The server returns
 * the tree as a flat list; assembling it into a nesting is the client's job.
 */
import { request } from './client';
import type { AuthorRooted } from './rooted';

/** One written version of a story. */
export type StoryVersion = {
  readonly id: string;
  readonly story_id: string;
  /** The version this one adapts, or null for a story's root version. */
  readonly parent_version_id: string | null;
  readonly author_id: string;
  /**
   * The author's display name and avatar path, attached by the server. The avatar
   * is a relative path on the API, or null when the author has no avatar (the
   * client then renders initials). Both are optional, because a response from an
   * older server omits them (KNOT-015b-fix).
   */
  readonly author_display_name?: string;
  readonly author_avatar_url?: string | null;
  readonly language: string;
  readonly title: string;
  readonly body: string;
  /** The adapter's note, or null when none was left. */
  readonly adaptation_note: string | null;
  readonly created_at: string;
  readonly updated_at: string;
  /**
   * The author's primary public Rooted signal, or null when they have none. The
   * server attaches it to the version, tree, and adapt responses.
   */
  readonly author_rooted?: AuthorRooted | null;
};

/** The request body for POST /stories/{id}/adapt. */
export type CreateAdaptationPayload = {
  /** The version being adapted. It must belong to the same story. */
  readonly parent_version_id: string;
  readonly language: string;
  readonly title: string;
  readonly body: string;
  readonly adaptation_note?: string;
};

/** The body returned by POST /stories/{id}/adapt and GET /versions/{id}. */
export type VersionResponse = {
  readonly version: StoryVersion;
};

/** The body returned by GET /stories/{id}/tree. */
export type LanguageTreeResponse = {
  readonly story_id: string;
  /** Every version of the story, root included, oldest first. */
  readonly versions: readonly StoryVersion[];
};

/** The story version endpoints. */
export const versionsApi = {
  /**
   * POST /stories/{id}/adapt — records a human adaptation of a version.
   *
   * The adapter is taken from `token` by the server; it is never sent in the
   * body. `payload.parent_version_id` must belong to the same story.
   */
  createAdaptation(
    storyId: string,
    token: string,
    payload: CreateAdaptationPayload,
  ): Promise<VersionResponse> {
    return request<VersionResponse>(`/stories/${encodeURIComponent(storyId)}/adapt`, {
      method: 'POST',
      body: payload,
      token,
    });
  },

  /** GET /stories/{id}/tree — every version of a story. Public. */
  getTree(storyId: string): Promise<LanguageTreeResponse> {
    return request<LanguageTreeResponse>(`/stories/${encodeURIComponent(storyId)}/tree`, {
      method: 'GET',
    });
  },

  /** GET /versions/{id} — reads one version. Public. */
  getVersion(id: string): Promise<VersionResponse> {
    return request<VersionResponse>(`/versions/${encodeURIComponent(id)}`, { method: 'GET' });
  },
};

/**
 * Returns each version's depth in the tree: 0 for the root, 1 for its children,
 * and so on.
 *
 * The traversal is bounded by a seen-set so a malformed parent cycle (which the
 * schema forbids) can never hang the screen.
 */
export function versionDepths(versions: readonly StoryVersion[]): Map<string, number> {
  const byId = new Map(versions.map((version) => [version.id, version]));
  const depths = new Map<string, number>();

  for (const version of versions) {
    let depth = 0;
    let parentId = version.parent_version_id;
    const seen = new Set<string>([version.id]);

    while (parentId !== null && byId.has(parentId) && !seen.has(parentId)) {
      seen.add(parentId);
      depth += 1;
      parentId = byId.get(parentId)?.parent_version_id ?? null;
    }

    depths.set(version.id, depth);
  }

  return depths;
}

/**
 * The story endpoints of the Knot HTTP API.
 *
 * Everything here goes through the shared transport in `client.ts`, so error
 * handling is identical to the auth endpoints: a failure is always an `ApiError`
 * carrying the server's machine-readable code.
 */
import { request, uploadFile } from './client';
import type { ReactionCounts } from './reactions';
import type { AuthorRooted } from './rooted';
import { MAX_IMAGE_BYTES, MAX_VIDEO_BYTES } from '../config/dev';

/** The two storytelling lenses a story can be filed under. */
export type Pillar = 'wonder' | 'heritage';

/** The pillar values, in the order the UI offers them. */
export const PILLARS: readonly Pillar[] = ['wonder', 'heritage'];

/** The coarse kind of a piece of story media. */
export type MediaType = 'image' | 'video';

/** How a piece of story media was obtained. */
export type MediaSource = 'camera' | 'gallery';

/** A piece of media attached to a story. */
export type StoryMedia = {
  readonly id: string;
  readonly media_type: MediaType;
  readonly source: MediaSource;
  readonly mime_type: string;
  readonly width: number | null;
  readonly height: number | null;
  readonly duration_ms: number | null;
  readonly size_bytes: number;
  readonly display_order: number;
  readonly created_at: string;
  /**
   * The path on the API that serves the bytes. It is a relative path
   * (`/stories/{id}/media/{mid}/content`); resolve it against the API base URL.
   */
  readonly url: string;
};

/** A story as returned by the API. */
export type Story = {
  readonly id: string;
  readonly author_id: string;
  /**
   * The author's display name and avatar path, attached by the server so a card
   * can attribute the story without a second request. `author_avatar_url` is a
   * relative path on the API, or null when the author has no avatar (the client
   * then renders initials).
   *
   * Both are optional because the API types describe the wire format
   * optimistically: a response from an older server omits them, and a client must
   * treat a missing field as absent rather than assume a string (KNOT-015b-fix).
   */
  readonly author_display_name?: string;
  readonly author_avatar_url?: string | null;
  /**
   * The id of the story's root version. The content fields below (language,
   * title, body) are the root version's content; adapting a story starts from
   * this version.
   */
  readonly root_version_id: string;
  readonly pillar: Pillar;
  readonly language: string;
  readonly title: string;
  readonly body: string;
  readonly approximate_location: string;
  /**
   * The structured place data, or null when the author attached no place. The
   * coordinate pair is null together or set together; the Discovery Map plots it
   * directly (KNOT-ADR-034).
   */
  readonly latitude?: number | null;
  readonly longitude?: number | null;
  readonly place_country?: string | null;
  readonly media_urls: readonly string[];
  readonly created_at: string;
  readonly updated_at: string;
  /**
   * The author's primary public Rooted signal, or null when they have none. The
   * server attaches it to the story detail response.
   */
  readonly author_rooted?: AuthorRooted | null;
  /**
   * The counts of each perspective signal on the story (KNOT-ADR-051). Optional
   * because an older server omits it, and because the API types describe the wire
   * format optimistically.
   */
  readonly reactions?: ReactionCounts;
  /** The signals the signed-in reader holds, as reaction-type strings. */
  readonly my_reactions?: readonly string[];
  /**
   * The story's attached media. On the detail it is the full list in display
   * order; on the feed it is a single-item preview (or an empty array). Always
   * present on responses from the current server, never null.
   */
  readonly media?: readonly StoryMedia[];
  /**
   * The story's aggregate counts, attached to both the feed and the detail by the
   * server's batched enrichment (KNOT-020c-fix). They are optional because the
   * API types describe the wire format optimistically: a response from an older
   * server omits them, and the client treats a missing count as zero.
   */
  readonly version_count?: number;
  readonly language_count?: number;
  readonly comment_count?: number;
  readonly media_count?: number;
  /**
   * The first media item's kind and its relative path on the API, or null when
   * the story has no media. The feed uses them so a video preview can be drawn as
   * a placeholder (no poster frame is stored yet) and an image rendered directly.
   */
  readonly first_media_type?: 'image' | 'video' | null;
  readonly first_media_url?: string | null;
  /**
   * The first media item's pixel dimensions, or null when the uploader's client
   * did not report them. The feed frames the media at its natural width:height
   * ratio from these, so a portrait photo is shown whole rather than cropped into
   * a fixed frame; both null falls back to 16:9 (KNOT-020g-fix5).
   */
  readonly first_media_width?: number | null;
  readonly first_media_height?: number | null;
  /**
   * Whether `body` is longer than the preview window a feed entry draws, so the
   * entry can offer a "read more" link. The server computes it from the body
   * length, so the client never has to infer truncation from the text
   * (KNOT-020g-fix12). Optional because the API types describe the wire format
   * optimistically; when absent the client falls back to inspecting the text.
   */
  readonly preview_truncated?: boolean;
  /**
   * The root version's attribution sentence, e.g. "Original story by Nomsa Khan
   * in English." A story IS its root version, so this is always the original's
   * line and `version_depth` is 0 (KNOT-021). Optional because the API types
   * describe the wire format optimistically: the server sends them on the story
   * detail only, so a feed entry sees them absent, and an older server omits
   * them entirely.
   */
  readonly attribution_line?: string;
  readonly version_depth?: number;
};

/**
 * One entry of the merged feed: a story, or somebody's adaptation of one
 * (KNOT-022).
 *
 * The two kinds are told apart by `kind`, and each carries the fields its own card
 * needs. Both are flat on the wire — `StoryFeedItem` is a `Story` plus the
 * discriminator, and `VersionFeedItem` repeats the story-shaped fields a card
 * renders — so a client switches on `kind` once and then reads the shape it picked.
 */
export type FeedItem = StoryFeedItem | VersionFeedItem;

/** A story entry in the feed: the story, marked as one. */
export type StoryFeedItem = Story & {
  readonly kind: 'story';
};

/**
 * An adaptation entry in the feed: one version somebody wrote of somebody else's
 * story.
 *
 * It carries the story-shaped fields a card renders (author, framing, media, counts,
 * reactions) so the feed's card can draw it, but its identity is the VERSION: `id`
 * is the version to open, `story_id` is the story it adapts, and the language,
 * title, and body are the adapter's text. Place and media are inherited from that
 * story, because a version carries neither of its own.
 *
 * There is no `version_depth` here: a feed entry is never a root, so
 * `version_number` — the position a reader says out loud, counting the original as
 * 1 — is what a card shows.
 */
export type VersionFeedItem = {
  readonly kind: 'version';
  /** The version's own id: what the reader opens, reacts to, and adapts from. */
  readonly id: string;
  /** The story being adapted. */
  readonly story_id: string;
  /**
   * The version this one adapts. It is never null: a root version is the story, so
   * it is not a feed entry of its own.
   */
  readonly parent_version_id: string;
  /** The ADAPTER. The original author is named in `attribution_line`. */
  readonly author_id: string;
  readonly author_display_name?: string;
  readonly author_avatar_url?: string | null;
  readonly author_rooted?: AuthorRooted | null;
  /**
   * The ORIGINAL author of the story this version adapts -- the first link of its
   * lineage -- so a card can head the entry with "{name}'s original story" from
   * structured data rather than parsing `attribution_line` (KNOT-023a). The name is
   * empty when the lineage could not be resolved, and the avatar is null when the
   * original author has none.
   */
  readonly root_author_id?: string;
  readonly root_author_display_name?: string;
  readonly root_author_avatar_url?: string | null;
  /**
   * The author of the version this one adapts directly -- the lineage actor the
   * card credits first -- so the avatar stack shows the lineage rather than the
   * poster (who is the entry's own `author` and is drawn in the author line). For
   * a first adaptation this is the same person as the root author, and the card
   * renders only one avatar (KNOT-022-fix2).
   */
  readonly parent_author_id?: string;
  readonly parent_author_display_name?: string;
  readonly parent_author_avatar_url?: string | null;
  readonly language: string;
  readonly title: string;
  readonly body: string;
  /** The story's pillar, inherited: an adaptation of a heritage story is heritage. */
  readonly pillar: Pillar;
  /** The story's place and structured coordinates, inherited. */
  readonly approximate_location: string;
  readonly latitude?: number | null;
  readonly longitude?: number | null;
  readonly place_country?: string | null;
  readonly media_urls: readonly string[];
  readonly created_at: string;
  /**
   * The STORY's media, so an adaptation entry shows the pictures the story came
   * with. The card draws them from the first-media fields below.
   */
  readonly media_count?: number;
  readonly first_media_type?: 'image' | 'video' | null;
  readonly first_media_url?: string | null;
  readonly first_media_width?: number | null;
  readonly first_media_height?: number | null;
  /** The VERSION's own counts: a reader reacts to the adaptation they are reading. */
  readonly reactions?: ReactionCounts;
  readonly my_reactions?: readonly string[];
  /** The story's aggregates, so the entry can say how much adaptation it has. */
  readonly version_count?: number;
  readonly language_count?: number;
  readonly comment_count?: number;
  /** The entry's position in its lineage, counting the original as 1. */
  readonly version_number: number;
  /**
   * The server-rendered sentence tracing this version back to the original, shown
   * verbatim. Empty when the lineage could not be resolved; the card then shows no
   * line.
   */
  readonly attribution_line: string;
  /** Whether `body` is longer than the preview window a card draws. */
  readonly preview_truncated?: boolean;
};
export type CreateStoryPayload = {
  readonly pillar: Pillar;
  readonly language: string;
  readonly title: string;
  readonly body: string;
  readonly approximate_location?: string;
  /**
   * Structured place data from the location picker. The coordinate pair must be
   * supplied together or not at all (KNOT-ADR-034).
   */
  readonly latitude?: number;
  readonly longitude?: number;
  readonly place_country?: string;
  readonly media_urls?: readonly string[];
};

/** The body returned by POST /stories and GET /stories/{id}. */
export type StoryResponse = {
  readonly story: Story;
};

/**
 * The body returned by GET /discovery/places/{place}: that place's stories.
 *
 * The place page lists stories only, so it keeps the stories-shaped body. The feed
 * is a merged stream of stories and adaptations and answers with `items` instead
 * (see FeedListResponse).
 */
export type StoryListResponse = {
  readonly stories: readonly Story[];
  /**
   * The token that resumes the page after this one, or an empty string when there
   * are no more pages. The server always sends the field.
   */
  readonly next_cursor: string;
};

/**
 * The body returned by GET /stories: one page of the merged feed, newest first.
 *
 * Each item carries the `kind` that says which shape it is -- a story or an
 * adaptation of one -- so a client switches on it and never has to infer the kind
 * from which fields happen to be present (KNOT-022).
 */
export type FeedListResponse = {
  readonly items: readonly FeedItem[];
  readonly next_cursor: string;
};

/** Options for a feed page. */
export type ListStoriesOptions = {
  /** A cursor from a previous page. Omit it to start at the newest entry. */
  readonly cursor?: string;
  /**
   * How many entries to ask for. The server defaults to 20 and caps at 50, and an
   * entry is one story or one adaptation of a story (KNOT-022).
   */
  readonly limit?: number;
};

/** The page size the feed asks for. */
export const FEED_PAGE_SIZE = 20;

/**
 * Builds the query string for a feed request, omitting empty values so the
 * server applies its own defaults.
 */
function feedQuery(options: ListStoriesOptions): string {
  const parts: string[] = [];

  if (options.limit !== undefined) {
    parts.push(`limit=${encodeURIComponent(String(options.limit))}`);
  }
  if (options.cursor !== undefined && options.cursor !== '') {
    parts.push(`cursor=${encodeURIComponent(options.cursor)}`);
  }

  return parts.length === 0 ? '' : `?${parts.join('&')}`;
}

/** The body returned by POST /stories/{id}/media. */
export type StoryMediaResponse = {
  readonly media: StoryMedia;
};

/** Optional metadata a picker can report alongside the file. */
export type StoryMediaMetadata = {
  readonly width?: number;
  readonly height?: number;
  readonly durationMs?: number;
};

/** The file extensions the server accepts for story media, keyed by MIME type. */
const MEDIA_EXTENSIONS: Readonly<Record<string, string>> = {
  'image/jpeg': 'jpg',
  'image/png': 'png',
  'image/webp': 'webp',
  'video/mp4': 'mp4',
  'video/quicktime': 'mov',
};

/** Picks a filename for the multipart upload; the server re-sniffs the bytes. */
function mediaFileName(mimeType: string): string {
  return `story-media.${MEDIA_EXTENSIONS[mimeType] ?? 'bin'}`;
}

/**
 * Refuses a file the server's limit would reject, before the upload is started,
 * so an oversized file does not spend the user's data only to be refused.
 */
function assertWithinLimit(mimeType: string, fileSize: number): void {
  const limit = mimeType.startsWith('video/') ? MAX_VIDEO_BYTES : MAX_IMAGE_BYTES;
  if (fileSize > limit) {
    throw new Error(`That file is larger than the ${Math.round(limit / (1024 * 1024))} MB limit.`);
  }
}

/**
 * POST /stories/{id}/media — attaches one image or video to a story.
 *
 * The uploader is taken from `token` by the server; it is never sent in the body.
 * The response carries the stored media, including its stable `url`.
 *
 * `source` records whether the file came from the camera or the gallery, which is
 * what the capture badge is drawn from.
 */
export function uploadStoryMedia(
  storyId: string,
  uri: string,
  mimeType: string,
  source: MediaSource,
  token: string,
  fileSize: number,
  metadata: StoryMediaMetadata = {},
): Promise<StoryMedia> {
  assertWithinLimit(mimeType, fileSize);

  const fields: Record<string, string> = { source };
  if (metadata.width !== undefined) {
    fields.width = String(metadata.width);
  }
  if (metadata.height !== undefined) {
    fields.height = String(metadata.height);
  }
  if (metadata.durationMs !== undefined) {
    fields.duration_ms = String(metadata.durationMs);
  }

  return uploadFile<StoryMediaResponse>(`/stories/${encodeURIComponent(storyId)}/media`, {
    file: { uri, name: mediaFileName(mimeType), type: mimeType },
    fields,
    token,
  }).then((response) => response.media);
}

/** The story endpoints. */
export const storiesApi = {
  /**
   * POST /stories — publishes a story as the authenticated user.
   *
   * The author is taken from `token` by the server; it is never sent in the body.
   */
  createStory(token: string, payload: CreateStoryPayload): Promise<StoryResponse> {
    return request<StoryResponse>('/stories', { method: 'POST', body: payload, token });
  },

  /** GET /stories/{id} — reads one story. Public. A token hides blocked authors. */
  getStory(id: string, token?: string): Promise<StoryResponse> {
    return request<StoryResponse>(`/stories/${encodeURIComponent(id)}`, { method: 'GET', token });
  },

  /**
   * GET /stories — reads one page of the merged feed, newest first. Public.
   *
   * The page carries stories and adaptations in one stream, each tagged with its
   * `kind` (KNOT-022).
   */
  listStories(options: ListStoriesOptions = {}, token?: string): Promise<FeedListResponse> {
    return request<FeedListResponse>(`/stories${feedQuery(options)}`, { method: 'GET', token });
  },
};

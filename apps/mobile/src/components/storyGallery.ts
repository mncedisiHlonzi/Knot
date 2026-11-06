/**
 * Resolving a story's whole media gallery for the viewer (KNOT-020k-fix4).
 *
 * A surface that holds the story (Story Detail, Version Detail) already has its
 * media array and passes it straight through. A surface that holds a card preview
 * supplies that one item, so the viewer opens at once; `App` then reads the story in
 * the background and swaps the whole array in, which is what lets the reader swipe
 * to everything the story has (KNOT-020k-fix4, KNOT-020k-fix11, KNOT-020k-fix16).
 *
 * The helpers here are pure and take plain data, so the mapping and the index
 * lookup are covered by the renderer-less tests. The request itself lives with the
 * caller that owns the session (`App.tsx`), which keeps this module free of the
 * transport.
 */
import type { StoryMedia } from '../api/stories';
import type { ViewerMedia } from './MediaGalleryModal';

/**
 * Maps the API's media onto the viewer's item shape.
 *
 * Every field is validated before it is used: the API types describe the wire
 * format optimistically, so a response can carry a missing url or an unknown kind
 * (KNOT-015b-fix). An item without a usable url or kind is dropped rather than
 * drawn as an empty page, and a missing dimension becomes `null` — the viewer
 * treats that as "unconstrained" and lets the media report its own shape.
 */
export function toViewerMedia(media: readonly StoryMedia[] | null | undefined): ViewerMedia[] {
  if (!Array.isArray(media)) {
    return [];
  }

  const items: ViewerMedia[] = [];
  for (const item of media) {
    if (item === null || typeof item !== 'object') {
      continue;
    }
    const url = typeof item.url === 'string' ? item.url.trim() : '';
    if (url === '' || (item.media_type !== 'image' && item.media_type !== 'video')) {
      continue;
    }

    // The WebP display is copied as-is when the server sent one, and left
    // undefined otherwise; the viewer falls back to `url`. `url` itself is never
    // rewritten, because it is the identity a tapped card matches on.
    const displayWebPURL =
      typeof item.display_webp_url === 'string' ? item.display_webp_url.trim() : '';

    // The still the viewer draws while the full rendition downloads: a video's
    // extracted poster, or an image's thumbnail. Either can be absent, and an
    // absent still is what the loader is for (KNOT-042).
    const stillURL = stillPath(item);

    items.push({
      id: typeof item.id === 'string' && item.id !== '' ? item.id : url,
      media_type: item.media_type,
      url,
      display_webp_url: displayWebPURL === '' ? null : displayWebPURL,
      thumbnail_url: stillURL.image,
      poster_url: stillURL.poster,
      width: typeof item.width === 'number' ? item.width : null,
      height: typeof item.height === 'number' ? item.height : null,
      duration_ms: typeof item.duration_ms === 'number' ? item.duration_ms : null,
    });
  }

  return items;
}

/**
 * The still addresses an item carries, each only when the server sent a non-empty
 * path.
 *
 * A video has a poster and no thumbnail; an image has a thumbnail and no poster.
 * They are read independently rather than switched on the media type, because the
 * wire is the authority: a row produced before KNOT-042 has neither (KNOT-042).
 */
function stillPath(item: StoryMedia): { image: string | null; poster: string | null } {
  const thumbnail = typeof item.thumbnail_url === 'string' ? item.thumbnail_url.trim() : '';
  const poster = typeof item.poster_url === 'string' ? item.poster_url.trim() : '';

  return {
    image: thumbnail === '' ? null : thumbnail,
    poster: poster === '' ? null : poster,
  };
}

/**
 * The page a tapped item occupies in the gallery, or `-1` when it is not there.
 *
 * The url is the only identity a card and the story's media array share — a feed
 * preview carries the path but not the media id — so the match is made on it. A
 * caller that gets `-1` opens on the first page rather than refusing to open.
 */
export function viewerIndexByUrl(items: readonly ViewerMedia[], url?: string | null): number {
  const target = typeof url === 'string' ? url.trim() : '';
  if (target === '') {
    return -1;
  }
  return items.findIndex((item) => item.url === target);
}

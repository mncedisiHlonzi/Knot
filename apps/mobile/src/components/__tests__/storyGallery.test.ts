import type { StoryMedia } from '../../api/stories';
import type { ViewerStory } from '../MediaGalleryModal';
import { toViewerMedia, viewerIndexByUrl } from '../storyGallery';

/** One story media row, with only the fields the mapper reads overridable. */
function storyMedia(over: Partial<StoryMedia> = {}): StoryMedia {
  return {
    id: 'media-1',
    media_type: 'image',
    source: 'gallery',
    mime_type: 'image/jpeg',
    width: 1200,
    height: 800,
    duration_ms: null,
    size_bytes: 1024,
    display_order: 0,
    created_at: '2026-01-01T00:00:00Z',
    url: '/stories/story-1/media/media-1/content',
    ...over,
  };
}

describe('toViewerMedia', () => {
  it('maps every item of the gallery in display order', () => {
    const items = toViewerMedia([
      storyMedia(),
      storyMedia({
        id: 'media-2',
        media_type: 'video',
        mime_type: 'video/mp4',
        duration_ms: 42000,
        display_order: 1,
        url: '/stories/story-1/media/media-2/content',
      }),
    ]);

    expect(items).toHaveLength(2);
    expect(items[0]).toEqual({
      id: 'media-1',
      media_type: 'image',
      url: '/stories/story-1/media/media-1/content',
      width: 1200,
      height: 800,
      duration_ms: null,
    });
    expect(items[1].media_type).toBe('video');
    expect(items[1].duration_ms).toBe(42000);
  });

  it('treats unknown dimensions as unconstrained rather than zero', () => {
    const [item] = toViewerMedia([storyMedia({ width: null, height: null })]);
    expect(item.width).toBeNull();
    expect(item.height).toBeNull();
  });

  it('drops an item it could not draw', () => {
    const items = toViewerMedia([
      storyMedia({ url: '   ' }),
      storyMedia({ media_type: 'audio' as unknown as StoryMedia['media_type'] }),
      storyMedia({ id: 'media-3', url: '/stories/story-1/media/media-3/content' }),
    ]);

    expect(items).toHaveLength(1);
    expect(items[0].id).toBe('media-3');
  });

  it('returns nothing when there is no gallery', () => {
    expect(toViewerMedia(undefined)).toEqual([]);
    expect(toViewerMedia(null)).toEqual([]);
    expect(toViewerMedia([])).toEqual([]);
  });
});

describe('viewerIndexByUrl', () => {
  const items = toViewerMedia([
    storyMedia(),
    storyMedia({ id: 'media-2', url: '/stories/story-1/media/media-2/content' }),
  ]);

  it('finds the page the tapped preview belongs to', () => {
    expect(viewerIndexByUrl(items, '/stories/story-1/media/media-2/content')).toBe(1);
    expect(viewerIndexByUrl(items, `  ${items[0].url}  `)).toBe(0);
  });

  it('reports an item the gallery does not hold instead of guessing', () => {
    expect(viewerIndexByUrl(items, '/somewhere/else')).toBe(-1);
    expect(viewerIndexByUrl(items, '')).toBe(-1);
    expect(viewerIndexByUrl([], items[0].url)).toBe(-1);
  });
});

describe('ViewerStory', () => {
  it('carries the whole gallery, so the viewer can page through the story', () => {
    const media = toViewerMedia([
      storyMedia(),
      storyMedia({ id: 'media-2', url: '/stories/story-1/media/media-2/content' }),
    ]);
    const story: ViewerStory = {
      storyId: 'story-1',
      title: 'A story',
      body: 'The opening lines.',
      media,
    };

    expect(story.media).toHaveLength(2);
    expect(story.media[1].url).toBe('/stories/story-1/media/media-2/content');
  });
});

import type { FeedItem } from '../../../api/stories';
import { entryStoryMap, feedEntryStory } from '../feedEntryProps';

/**
 * The screen used to fold each entry onto the card's own story shape inside
 * `renderItem`, so every list render produced a new object and re-rendered the
 * card. The fold now happens once per item, and these tests pin the two things
 * that makes true: the shape it produces, and that one list yields one object
 * per entry (KNOT-049).
 */

const STORY: FeedItem = {
  id: 'story-1',
  kind: 'story',
  author_id: 'user-1',
  author_display_name: 'Nomsa Khan',
  author_avatar_url: null,
  root_version_id: 'version-1',
  pillar: 'heritage',
  language: 'zap',
  title: 'How my grandmother counted the days',
  body: 'She kept a string of knots.',
  approximate_location: 'Oaxaca, Mexico',
  media_urls: [],
  created_at: '2026-10-09T12:00:00.000Z',
  updated_at: '2026-10-09T12:00:00.000Z',
  first_media_type: 'video',
  first_media_url: '/stories/story-1/media/m1/content?variant=stream',
  first_media_poster_url: '/stories/story-1/media/m1/content?variant=poster',
  media: [
    {
      id: 'm1',
      media_type: 'video',
      source: 'gallery',
      mime_type: 'video/mp4',
      width: 1080,
      height: 1920,
      duration_ms: 33000,
      size_bytes: 1,
      display_order: 0,
      created_at: '2026-10-09T12:00:00.000Z',
      url: '/stories/story-1/media/m1/content?variant=stream',
    },
  ],
};

const VERSION: FeedItem = {
  kind: 'version',
  id: 'version-2',
  story_id: 'story-1',
  parent_version_id: 'version-1',
  author_id: 'user-2',
  author_display_name: 'Bongani Ncube',
  language: 'zul',
  title: 'How my grandmother counted the days',
  body: 'She kept a string of knots.',
  pillar: 'heritage',
  approximate_location: 'Oaxaca, Mexico',
  media_urls: [],
  created_at: '2026-10-10T12:00:00.000Z',
  first_media_type: 'video',
  first_media_url: '/stories/story-1/media/m1/content?variant=stream',
  first_media_poster_url: '/stories/story-1/media/m1/content?variant=poster',
  root_author_id: 'user-1',
  root_author_display_name: 'Nomsa Khan',
  root_author_avatar_url: null,
  parent_author_id: 'user-1',
  parent_author_display_name: 'Nomsa Khan',
  parent_author_avatar_url: null,
  version_number: 2,
  attribution_line: "Adapted from Nomsa Khan's original story in Zapotec.",
};

describe('feedEntryStory', () => {
  it('carries the poster, the duration and the place a story card draws', () => {
    const story = feedEntryStory(STORY);

    expect(story.first_media_poster_url).toBe('/stories/story-1/media/m1/content?variant=poster');
    expect(story.duration_ms).toBe(33000);
    expect(story.storyId).toBe('story-1');
    expect(story.place).toBe('Oaxaca, Mexico');
    // A story entry is its own story; the lineage fields belong to adaptations.
    expect(story.root_author_id).toBeUndefined();
  });

  it('carries the lineage and a null duration for an adaptation', () => {
    const version = feedEntryStory(VERSION);

    expect(version.storyId).toBe('story-1');
    expect(version.root_author_id).toBe('user-1');
    expect(version.parent_author_display_name).toBe('Nomsa Khan');
    // A version payload has no media array, so there is no duration on the wire.
    expect(version.duration_ms).toBeNull();
  });
});

describe('entryStoryMap', () => {
  it('prepares exactly one story object per entry, keyed by id', () => {
    const stories = entryStoryMap([STORY, VERSION]);

    expect(stories.size).toBe(2);
    expect(stories.get('story-1')?.title).toBe(STORY.title);
    expect(stories.get('version-2')?.title).toBe(VERSION.title);
  });

  it('maps one item to equal values each time, so a rebuilt map is still equal', () => {
    const first = feedEntryStory(STORY);
    const second = feedEntryStory(STORY);

    // Different objects (the screen's memo is what keeps one identity), but equal
    // data — which the memoised card's field comparison accepts, so a refresh
    // that changed nothing re-renders nothing.
    expect(second).not.toBe(first);
    expect(second).toEqual(first);
  });
});

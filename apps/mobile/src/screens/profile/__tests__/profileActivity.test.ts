import type { Activity } from '../../../api/profile';
import {
  DEFAULT_PROFILE_TAB,
  PROFILE_TABS,
  activityCard,
  filterActivities,
  profileStats,
  profileTabEmptyHeadline,
  statsLine,
} from '../profileActivity';

const story: Activity = {
  kind: 'story',
  id: 'story-1',
  created_at: '2025-03-01T00:00:00Z',
  payload: { title: 'How my grandmother counted the days', pillar: 'heritage', language: 'eng' },
};

const version: Activity = {
  kind: 'version',
  id: 'version-1',
  created_at: '2025-03-02T00:00:00Z',
  payload: { story_id: 'story-1', story_title: 'How my grandmother…', language: 'zul' },
};

const comment: Activity = {
  kind: 'comment',
  id: 'comment-1',
  created_at: '2025-03-03T00:00:00Z',
  payload: { version_id: 'version-1', story_id: 'story-1', body_preview: 'Yes.' },
};

const bridge: Activity = {
  kind: 'bridge',
  id: 'bridge-1',
  created_at: '2025-03-04T00:00:00Z',
  payload: { source_comment_id: 'comment-1', version_id: 'version-1', target_language: 'zul' },
};

const inquiry: Activity = {
  kind: 'inquiry',
  id: 'inquiry-1',
  created_at: '2025-03-05T00:00:00Z',
  payload: { title: 'Why the stairs are uneven' },
};

const answer: Activity = {
  kind: 'inquiry_answer',
  id: 'answer-1',
  created_at: '2025-03-06T00:00:00Z',
  payload: {
    inquiry_id: 'inquiry-1',
    inquiry_title: 'Why the stairs are uneven',
    body_preview: 'Talus.',
  },
};

const wall: readonly Activity[] = [story, version, comment, bridge, inquiry, answer];

describe('profile tabs', () => {
  it('lists the three tabs in order, starting on stories', () => {
    expect(PROFILE_TABS.map((tab) => tab.label)).toEqual(['Stories', 'Versions', 'Inquiries']);
    expect(DEFAULT_PROFILE_TAB).toBe('story');
  });

  it('filters on the three authored kinds only', () => {
    expect(PROFILE_TABS.map((tab) => tab.kind)).toEqual(['story', 'version', 'inquiry']);
  });
});

describe('filterActivities', () => {
  it('keeps only the activities of the requested kind, in order', () => {
    expect(filterActivities(wall, 'story')).toEqual([story]);
    expect(filterActivities(wall, 'version')).toEqual([version]);
    expect(filterActivities(wall, 'comment')).toEqual([comment]);
    expect(filterActivities(wall, 'bridge')).toEqual([bridge]);
    expect(filterActivities(wall, 'inquiry')).toEqual([inquiry]);
    expect(filterActivities(wall, 'inquiry_answer')).toEqual([answer]);
  });

  it('returns an empty list when nothing matches', () => {
    expect(filterActivities([story], 'bridge')).toEqual([]);
  });
});

describe('profileStats', () => {
  it('counts stories, versions, and answers only', () => {
    expect(profileStats(wall)).toEqual({ stories: 1, versions: 1, answers: 1 });
  });

  it('is all zeros for an empty wall', () => {
    expect(profileStats([])).toEqual({ stories: 0, versions: 0, answers: 0 });
  });

  it('formats the stats line with the design copy', () => {
    expect(statsLine({ stories: 12, versions: 8, answers: 21 })).toBe(
      '12 stories · 8 versions · 21 answers',
    );
  });
});

describe('activityCard', () => {
  it('builds a story card from its pillar, language, and time', () => {
    expect(activityCard(story, '2 days ago')).toEqual({
      overline: '',
      title: 'How my grandmother counted the days',
      pillar: 'heritage',
      meta: 'English · 2 days ago',
    });
  });

  it('drops a pillar it does not recognise', () => {
    const odd: Activity = {
      ...story,
      payload: { ...story.payload, pillar: 'mystery' },
    } as Activity;
    expect(activityCard(odd, 'now')?.pillar).toBeUndefined();
  });

  it('builds a version card naming the language it was told in', () => {
    expect(activityCard(version, '3 weeks ago')).toEqual({
      overline: 'Version · Zulu',
      title: 'How my grandmother…',
      pillar: undefined,
      meta: '3 weeks ago',
    });
  });

  it('builds an inquiry card, reading a place-less question as just "Inquiry"', () => {
    expect(activityCard(inquiry, '1 hour ago')).toEqual({
      overline: 'Inquiry',
      title: 'Why the stairs are uneven',
      pillar: undefined,
      meta: '1 hour ago',
    });
  });

  it('names the place on an inquiry card when the payload carries one', () => {
    const placed: Activity = {
      ...inquiry,
      payload: { title: inquiry.payload.title, place: 'Oaxaca, Mexico' },
    } as Activity;
    expect(activityCard(placed, '1 hour ago')?.overline).toBe('Inquiry · Oaxaca, Mexico');
  });

  it('is undefined for a kind the wall does not show', () => {
    expect(activityCard(comment, 'now')).toBeUndefined();
    expect(activityCard(bridge, 'now')).toBeUndefined();
    expect(activityCard(answer, 'now')).toBeUndefined();
  });
});

describe('profileTabEmptyHeadline', () => {
  it('names the tab', () => {
    expect(profileTabEmptyHeadline('story')).toBe('No stories yet.');
    expect(profileTabEmptyHeadline('version')).toBe('No versions yet.');
    expect(profileTabEmptyHeadline('inquiry')).toBe('No inquiries yet.');
  });

  it('falls back for a kind without a tab', () => {
    expect(profileTabEmptyHeadline('comment')).toBe('Nothing here yet.');
  });
});

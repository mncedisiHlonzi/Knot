import type { Activity } from '../../../api/profile';
import {
  DEFAULT_PROFILE_TAB,
  PROFILE_TABS,
  filterActivities,
  profileStats,
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
  it('lists the six tabs in order, starting on stories', () => {
    expect(PROFILE_TABS.map((tab) => tab.label)).toEqual([
      'Stories',
      'Versions',
      'Comments',
      'Bridges',
      'Inquiries',
      'Answers',
    ]);
    expect(DEFAULT_PROFILE_TAB).toBe('story');
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

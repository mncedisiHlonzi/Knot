/**
 * Profile activity-wall logic (KNOT-020c).
 *
 * The Profile screen groups a person's wall into six tabs and shows a headline
 * stats row. Both are pure reductions over the activities the wall has loaded, so
 * they live here rather than in the screen and are covered without a renderer.
 *
 * IMPORTANT: the profile API (`GET /users/{id}/profile`) returns one page of
 * activities and no totals, so the stats and the per-tab lists are computed over
 * the activities currently loaded — they grow as the wall is paged. This is a
 * client-side approximation, not a server count (KNOT-020c disclosure).
 */
import type { Activity, ActivityKind } from '../../api/profile';

/** One activity tab: the kind it filters to and the word shown on it. */
export type ProfileTab = {
  readonly kind: ActivityKind;
  readonly label: string;
};

/** The tabs, in the order they appear left to right. */
export const PROFILE_TABS: readonly ProfileTab[] = [
  { kind: 'story', label: 'Stories' },
  { kind: 'version', label: 'Versions' },
  { kind: 'comment', label: 'Comments' },
  { kind: 'bridge', label: 'Bridges' },
  { kind: 'inquiry', label: 'Inquiries' },
  { kind: 'inquiry_answer', label: 'Answers' },
];

/** The tab shown first. */
export const DEFAULT_PROFILE_TAB: ActivityKind = 'story';

/** The activities of one kind, in the order they were given (already newest-first). */
export function filterActivities(
  activities: readonly Activity[],
  kind: ActivityKind,
): readonly Activity[] {
  return activities.filter((activity) => activity.kind === kind);
}

/** The three headline counts shown in the profile's stats row. */
export type ProfileStats = {
  readonly stories: number;
  readonly versions: number;
  readonly answers: number;
};

/**
 * Counts the three headline kinds — stories, adaptations (versions), and answers
 * to questions — over the activities given.
 */
export function profileStats(activities: readonly Activity[]): ProfileStats {
  let stories = 0;
  let versions = 0;
  let answers = 0;

  for (const activity of activities) {
    if (activity.kind === 'story') {
      stories += 1;
    } else if (activity.kind === 'version') {
      versions += 1;
    } else if (activity.kind === 'inquiry_answer') {
      answers += 1;
    }
  }

  return { answers, stories, versions };
}

/**
 * The stats row's single line, e.g. "12 stories · 8 versions · 21 answers".
 *
 * The nouns stay plural even at one, matching the design copy.
 */
export function statsLine(stats: ProfileStats): string {
  return `${stats.stories} stories · ${stats.versions} versions · ${stats.answers} answers`;
}

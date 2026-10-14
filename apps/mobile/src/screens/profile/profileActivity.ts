/**
 * Profile activity-wall logic (KNOT-020c).
 *
 * The Profile screen groups a person's wall into three tabs and shows a headline
 * stats row. Both are pure reductions over the activities the wall has loaded, so
 * they live here rather than in the screen and are covered without a renderer.
 *
 * IMPORTANT: the profile API (`GET /users/{id}/profile`) returns one page of
 * activities and no totals, so the stats and the per-tab lists are computed over
 * the activities currently loaded — they grow as the wall is paged. This is a
 * client-side approximation, not a server count (KNOT-020c disclosure).
 */
import type { Activity, ActivityKind } from '../../api/profile';
import type { Pillar } from '../../components/PillarTag';
import { languageName } from '../../data/languages';

/** One activity tab: the kind it filters to and the word shown on it. */
export type ProfileTab = {
  readonly kind: ActivityKind;
  readonly label: string;
};

/**
 * The three tabs, in the order they appear left to right.
 *
 * The wall shows only what a person authored themselves — the stories they told,
 * the versions they adapted, and the questions they asked. Comments, bridges, and
 * answers are still on the activity stream (the API is unchanged); they are simply
 * not given a tab (KNOT-020g-fix).
 */
export const PROFILE_TABS: readonly ProfileTab[] = [
  { kind: 'story', label: 'Stories' },
  { kind: 'version', label: 'Versions' },
  { kind: 'inquiry', label: 'Inquiries' },
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

/** The pillars the tag accepts. Anything else is dropped rather than rendered. */
function asPillar(value: string | undefined): Pillar | undefined {
  return value === 'wonder' || value === 'heritage' ? value : undefined;
}

/**
 * A payload string, trimmed, or '' when it is absent. The response types describe
 * an optimistic wire format, so a field may be missing at runtime (KNOT-015b-fix).
 */
function asText(value: string | undefined): string {
  return typeof value === 'string' ? value.trim() : '';
}

/**
 * A payload count as a number, or 0 when it is absent. `omitempty` on the server
 * drops a zero count from the wire entirely, so a missing count means zero.
 */
function asCount(value: number | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

/** Joins the parts that are present with " · ", dropping the empty ones. */
function joinParts(parts: readonly string[]): string {
  return parts.filter((part) => part !== '').join(' · ');
}

/** How many versions a story has, in words: "1 version" / "4 versions". */
function versionCountLabel(count: number): string {
  return count === 1 ? '1 version' : `${count} versions`;
}

/** How many answers a question has, in words: "1 answer" / "6 answers". */
function answerCountLabel(count: number): string {
  return count === 1 ? '1 answer' : `${count} answers`;
}

/**
 * The rendered content of one card on the wall.
 *
 * A card carries only what the activity's payload holds. The server enriches each
 * activity with the counts and places the reference draws (KNOT-020g-fix2), but
 * the payload is still a small, kind-specific subset, so a card renders only the
 * segments it has.
 */
export type ProfileCard = {
  /**
   * The overline above the title, or '' when the card has none — a story card
   * leads with its pillar tag instead.
   */
  readonly overline: string;
  /** The card's title. */
  readonly title: string;
  /** The pillar, for the story card's plain pillar tag; undefined otherwise. */
  readonly pillar: Pillar | undefined;
  /**
   * The place a story's pillar tag shows, when the payload carries one. A version's
   * place lives in the meta line and an inquiry's in the overline, so this is
   * undefined for both.
   */
  readonly place: string | undefined;
  /** The meta line under the title, or '' when there is nothing to show. */
  readonly meta: string;
};

/**
 * The card for one activity, or undefined for a kind the wall does not show.
 *
 * A **story** card leads with its plain pillar tag — `PILLAR · PLACE` when the
 * story names a place — over the title, and its meta reads
 * `{language} · {N versions} · {time}`. A **version** card leads with
 * `Version {n} · {language}` over the story's title, and its meta reads
 * `{place} · {time}`. An **inquiry** card leads with `Inquiry · {place}` over the
 * question, and its meta reads `{N answers} · {time}`. A segment whose field is
 * absent is omitted, never faked.
 *
 * `relativeTime` is passed in already formatted, so this stays a pure function of
 * its inputs and is testable without a wall clock.
 */
export function activityCard(activity: Activity, relativeTime: string): ProfileCard | undefined {
  switch (activity.kind) {
    case 'story': {
      const place = asText(activity.payload.place);
      const versions = asCount(activity.payload.version_count);
      return {
        overline: '',
        title: activity.payload.title,
        pillar: asPillar(activity.payload.pillar),
        place: place === '' ? undefined : place,
        meta: joinParts([
          languageName(activity.payload.language),
          versions > 0 ? versionCountLabel(versions) : '',
          relativeTime,
        ]),
      };
    }
    case 'version': {
      const language = languageName(activity.payload.language);
      const number = asCount(activity.payload.version_number);
      return {
        overline: number > 0 ? `Version ${number} · ${language}` : `Version · ${language}`,
        title: activity.payload.story_title,
        pillar: undefined,
        place: undefined,
        meta: joinParts([asText(activity.payload.place), relativeTime]),
      };
    }
    case 'inquiry': {
      const place = asText(activity.payload.place);
      return {
        overline: place === '' ? 'Inquiry' : `Inquiry · ${place}`,
        title: activity.payload.title,
        pillar: undefined,
        place: undefined,
        meta: joinParts([answerCountLabel(asCount(activity.payload.answer_count)), relativeTime]),
      };
    }
    default:
      return undefined;
  }
}

/** The empty state's headline for one tab. */
export function profileTabEmptyHeadline(kind: ActivityKind): string {
  switch (kind) {
    case 'story':
      return 'No stories yet.';
    case 'version':
      return 'No versions yet.';
    case 'inquiry':
      return 'No inquiries yet.';
    default:
      return 'Nothing here yet.';
  }
}

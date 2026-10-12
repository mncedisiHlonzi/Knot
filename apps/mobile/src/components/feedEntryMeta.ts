import type { ReactionCounts } from '../api/reactions';

/** A pluralised count label, e.g. "1 version", "4 versions". */
export function countLabel(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`;
}

/** The total of the four perspective signals on one entity. */
export function totalReactions(counts: ReactionCounts): number {
  return (
    counts.rings_true +
    counts.know_it_differently +
    counts.adds_something_new +
    counts.needs_a_source
  );
}

/** The pieces a feed entry's meta row needs, already reduced to what is shown. */
export type FeedMeta = {
  /** The version segment, or an empty string when there are none. */
  readonly versions: string;
  /** The reactions-and-comments suffix, joined with " · " (empty when neither). */
  readonly suffix: string;
};

/**
 * Builds the two meta-row segments a feed entry shows: the version count, and the
 * reactions/comment suffix.
 *
 * A zero count is omitted rather than shown as "0 versions", so an entry with no
 * reactions yet reads as a quiet row rather than a tally of nothing. The suffix is
 * a single string so it can be rendered as one text node beside the (tappable)
 * version count.
 */
export function feedMeta(input: {
  readonly versionCount?: number;
  readonly reactions?: ReactionCounts;
  readonly commentCount?: number;
}): FeedMeta {
  const versions = input.versionCount ?? 0;
  const reactions = input.reactions === undefined ? 0 : totalReactions(input.reactions);
  const comments = input.commentCount ?? 0;

  const suffix: string[] = [];
  if (reactions > 0) {
    suffix.push(countLabel(reactions, 'reaction', 'reactions'));
  }
  if (comments > 0) {
    suffix.push(countLabel(comments, 'comment', 'comments'));
  }

  return {
    versions: versions > 0 ? countLabel(versions, 'version', 'versions') : '',
    suffix: suffix.join(' · '),
  };
}

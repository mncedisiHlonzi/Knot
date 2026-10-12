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

/** The kinds of segment a feed entry's meta row can show, in order. */
export type FeedMetaSegmentKey = 'versions' | 'languages' | 'reactions' | 'comments';

/** One piece of the meta row: its label and whether it leads with a glyph. */
export type FeedMetaSegment = {
  readonly key: FeedMetaSegmentKey;
  /** The text to render, e.g. "4 versions", "3 languages", "128", "14 comments". */
  readonly label: string;
  /**
   * The spoken form, which spells the number out in words where the visible label
   * is a bare total (the reaction segment reads as "128 reactions" to a screen
   * reader, not "128").
   */
  readonly accessibilityLabel: string;
  /**
   * True for the reaction segment, which draws the rings-true glyph before the
   * bare total rather than a worded label.
   */
  readonly withGlyph: boolean;
};

/**
 * Builds the meta row's segments from a story's counts, in the order they appear:
 * versions, languages, reactions, comments.
 *
 * A zero count is omitted rather than shown as "0 versions", so an entry with no
 * activity reads as a quiet row rather than a tally of nothing. An empty result
 * means there is nothing to show and the caller renders no meta row at all (the
 * relative time is already carried by the author line). The reaction segment is a
 * bare total paired with the rings-true glyph; the rest are pluralised labels via
 * {@link countLabel}.
 */
export function feedMetaSegments(input: {
  readonly versionCount?: number;
  readonly languageCount?: number;
  readonly reactions?: ReactionCounts;
  readonly commentCount?: number;
}): readonly FeedMetaSegment[] {
  const segments: FeedMetaSegment[] = [];

  const versions = input.versionCount ?? 0;
  if (versions > 0) {
    const label = countLabel(versions, 'version', 'versions');
    segments.push({ accessibilityLabel: label, key: 'versions', label, withGlyph: false });
  }

  const languages = input.languageCount ?? 0;
  if (languages > 0) {
    const label = countLabel(languages, 'language', 'languages');
    segments.push({ accessibilityLabel: label, key: 'languages', label, withGlyph: false });
  }

  const reactions = input.reactions === undefined ? 0 : totalReactions(input.reactions);
  if (reactions > 0) {
    segments.push({
      accessibilityLabel: countLabel(reactions, 'reaction', 'reactions'),
      key: 'reactions',
      label: String(reactions),
      withGlyph: true,
    });
  }

  const comments = input.commentCount ?? 0;
  if (comments > 0) {
    const label = countLabel(comments, 'comment', 'comments');
    segments.push({ accessibilityLabel: label, key: 'comments', label, withGlyph: false });
  }

  return segments;
}

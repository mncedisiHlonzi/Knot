import type { ReactionCounts } from '../api/reactions';

/** A pluralised count label, e.g. "1 version", "4 versions". */
export function countLabel(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`;
}

/**
 * Whether a preview string was cut short, judged from the text itself.
 *
 * The profile wall's excerpt is cut in SQL and given an ellipsis, so the text
 * carries its own "there is more" mark. This is the **fallback** for a response
 * that predates the `preview_truncated` flag: when the server does send the flag,
 * that answer wins, because a body could legitimately end in an ellipsis without
 * having been cut (KNOT-020g-fix12).
 */
export function isPreviewTruncated(preview: string): boolean {
  return preview.trimEnd().endsWith('…');
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

/**
 * Builds a feed entry's meta line: the story's counts as plain text, in the order
 * versions, languages, reactions, comments, joined by " · ".
 *
 * Every segment is a worded, pluralised label — "4 versions · 3 languages ·
 * 128 reactions · 14 comments" — so the line reads as prose and needs no glyph or
 * per-segment styling (KNOT-020c-fix2). The reaction segment is the total of the
 * four perspective signals, spelled out rather than drawn as an icon.
 *
 * A zero count is omitted rather than shown as "0 versions", so an entry with no
 * activity reads as a quiet row rather than a tally of nothing. An **empty string**
 * means there is nothing to show and the caller renders no meta row at all (the
 * relative time is already carried by the author line).
 */
export function feedMetaLine(input: {
  readonly versionCount?: number;
  readonly languageCount?: number;
  readonly reactions?: ReactionCounts;
  readonly commentCount?: number;
}): string {
  const segments: string[] = [];

  const versions = input.versionCount ?? 0;
  if (versions > 0) {
    segments.push(countLabel(versions, 'version', 'versions'));
  }

  const languages = input.languageCount ?? 0;
  if (languages > 0) {
    segments.push(countLabel(languages, 'language', 'languages'));
  }

  const reactions = input.reactions === undefined ? 0 : totalReactions(input.reactions);
  if (reactions > 0) {
    segments.push(countLabel(reactions, 'reaction', 'reactions'));
  }

  const comments = input.commentCount ?? 0;
  if (comments > 0) {
    segments.push(countLabel(comments, 'comment', 'comments'));
  }

  return segments.join(' · ');
}

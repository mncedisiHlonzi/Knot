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
 * One attribution sentence split so its author name can be drawn in a heavier
 * voice than the rest. `prefix` and `suffix` are plain text; `name` is the span a
 * card may bold.
 */
export type AttributionParts = {
  readonly prefix: string;
  readonly name: string;
  readonly suffix: string;
};

/**
 * Splits the server's attribution sentence around the author it names, for
 * display only (KNOT-023c).
 *
 * The server renders two shapes that begin with a person:
 *
 *   Original story by {name} in {language}.
 *   Adapted from {name}'s ... (a root adaptation, or a deeper version's parent)
 *
 * The name is the first span after `from ` up to `'s `, or after `by ` up to
 * ` in `. Both patterns are deliberately NON-GREEDY: a deeper lineage reads
 * "Adapted from Bongani's Zulu version of Ada's original story …", and the first
 * name (Bongani) is the one the sentence opens with; a greedy match would swallow
 * the rest of the sentence. Names containing the literal `'s ` or ` in ` are the
 * only ambiguous input, and they degrade to the first plausible split rather than
 * failing.
 *
 * An unexpected shape returns `undefined`, which the card renders as the plain
 * sentence. Attribution is supplementary, so it must never fail to draw.
 */
export function attributionParts(line: string): AttributionParts | undefined {
  const value = line.trim();
  if (value === '') {
    return undefined;
  }

  const adapted = /^Adapted from (.+?)'s (.+)$/.exec(value);
  if (adapted !== null) {
    return { prefix: 'Adapted from ', name: adapted[1], suffix: `'s ${adapted[2]}` };
  }

  const original = /^Original story by (.+?) in (.+)$/.exec(value);
  if (original !== null) {
    return { prefix: 'Original story by ', name: original[1], suffix: ` in ${original[2]}` };
  }

  return undefined;
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

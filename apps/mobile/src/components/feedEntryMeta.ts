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

/** One run of an attribution sentence; `bold` marks a person's name. */
export type AttributionSegment = {
  readonly text: string;
  readonly bold: boolean;
};

/** The character range of one person's name inside an attribution sentence. */
type NameRange = {
  readonly start: number;
  readonly end: number;
};

/**
 * Splits the server's attribution sentence into text runs, marking every person
 * it names so a card can draw their names in a heavier voice (KNOT-022-fix1).
 *
 * The server renders these shapes:
 *
 *   Original story by {name} in {language}.
 *   Adapted from {name}'s original story in {language} into {language}.
 *   Adapted from {parent}'s {language} version of {root}'s original story …
 *   Adapted from {parent}'s {language} version, which traces back through
 *     {N} adaptations to {root}'s original story …
 *
 * The first name is the span after `Adapted from ` up to `'s `, or after
 * `Original story by ` up to ` in `. A second name, when the sentence has one, is
 * the span after ` of ` or ` to ` up to `'s original story`. All patterns are
 * deliberately NON-GREEDY, so a deeper lineage keeps the parent's name as the
 * first (the sentence opens with it) instead of swallowing the rest.
 *
 * Names containing the literal `'s ` (or ` in `, in the original-story shape) are
 * the only ambiguous input, and they degrade to the first plausible split rather
 * than failing. An unexpected shape returns the whole sentence as one plain run:
 * attribution is supplementary, so it must never fail to draw.
 */
export function attributionSegments(line: string): readonly AttributionSegment[] {
  const value = line.trim();
  if (value === '') {
    return [];
  }

  const ranges = nameRanges(value);
  if (ranges.length === 0) {
    return [{ text: value, bold: false }];
  }

  const segments: AttributionSegment[] = [];
  let cursor = 0;
  for (const range of ranges) {
    if (range.start > cursor) {
      segments.push({ text: value.slice(cursor, range.start), bold: false });
    }
    segments.push({ text: value.slice(range.start, range.end), bold: true });
    cursor = range.end;
  }
  if (cursor < value.length) {
    segments.push({ text: value.slice(cursor), bold: false });
  }

  return segments;
}

/** Finds the names an attribution sentence mentions, in reading order. */
function nameRanges(value: string): readonly NameRange[] {
  const original = /^Original story by (.+?) in /.exec(value);
  if (original !== null) {
    const start = 'Original story by '.length;
    return [{ start, end: start + original[1].length }];
  }

  const adapted = /^Adapted from (.+?)'s /.exec(value);
  if (adapted === null) {
    return [];
  }

  const firstNameStart = 'Adapted from '.length;
  const ranges: NameRange[] = [{ start: firstNameStart, end: firstNameStart + adapted[1].length }];

  // A deeper lineage names the original too: "…version of Ada's original
  // story…", or "…adaptations to Ada's original story…".
  const tail = value.slice(firstNameStart + adapted[1].length);
  const root = / (?:of|to) (.+?)'s original story/.exec(tail);
  if (root !== null) {
    const start = firstNameStart + adapted[1].length + tail.indexOf(root[1], root.index);
    ranges.push({ start, end: start + root[1].length });
  }

  return ranges;
}

/**
 * Splits the server's attribution sentence around the author it names, for
 * display only (KNOT-023c).
 *
 * It is the first bold run of `attributionSegments`, with the rest of the
 * sentence as its prefix and suffix. An unexpected shape returns `undefined`,
 * which a card renders as the plain sentence.
 */
export function attributionParts(line: string): AttributionParts | undefined {
  const segments = attributionSegments(line);
  const nameIndex = segments.findIndex((segment) => segment.bold);
  if (nameIndex === -1) {
    return undefined;
  }

  return {
    prefix: segments
      .slice(0, nameIndex)
      .map((segment) => segment.text)
      .join(''),
    name: segments[nameIndex].text,
    suffix: segments
      .slice(nameIndex + 1)
      .map((segment) => segment.text)
      .join(''),
  };
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

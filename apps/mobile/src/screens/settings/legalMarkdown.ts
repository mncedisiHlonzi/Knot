/**
 * A parser for the markdown subset the two legal documents use.
 *
 * The app deliberately carries no markdown library (KNOT-035). The documents are
 * founder-approved and fixed, and they use exactly four constructs: a level-1
 * title, level-2 section headings, `**bold**` runs, and `* ` bullets. A general
 * renderer would add a dependency and its own default typography — which the
 * design system forbids, because every style must come from the theme tokens.
 *
 * This module is pure and has no React Native import, so the parsing is unit
 * tested on its own and the screen only decides how each block looks.
 *
 * Anything this parser does not recognise is kept as body text rather than
 * dropped: a future edit to the documents degrades into readable prose instead of
 * silently losing a clause.
 */

/** One inline run of text: a stretch that is either bold or plain. */
export type LegalRun = {
  /** The run's text, verbatim. */
  readonly text: string;
  /** True when the run was wrapped in `**`. */
  readonly bold: boolean;
};

/** One rendered unit of a document. */
export type LegalBlock =
  | { readonly kind: 'heading'; readonly level: 1 | 2; readonly text: string }
  | { readonly kind: 'paragraph'; readonly runs: readonly LegalRun[] }
  | { readonly kind: 'bullet'; readonly runs: readonly LegalRun[] };

/** `## Heading` — checked before the level-1 form so the two cannot be confused. */
const HEADING_TWO = /^##\s+(.*)$/;
/** `# Heading` */
const HEADING_ONE = /^#\s+(.*)$/;
/** `* item` or `*   item`: the documents indent bullets with three spaces. */
const BULLET = /^\*\s+(.*)$/;

/**
 * Parses a markdown source into blocks.
 *
 * Blank lines end a paragraph. Consecutive non-blank lines are one paragraph and
 * are joined with a single space, which is what markdown itself does — the source
 * files are hard-wrapped at 80 columns and must not render with those breaks.
 */
export function parseLegalMarkdown(source: string): readonly LegalBlock[] {
  const blocks: LegalBlock[] = [];
  let paragraph: string[] = [];

  const flush = (): void => {
    if (paragraph.length === 0) {
      return;
    }
    blocks.push({ kind: 'paragraph', runs: inlineRuns(paragraph.join(' ')) });
    paragraph = [];
  };

  for (const rawLine of source.replace(/\r\n/g, '\n').split('\n')) {
    const line = rawLine.trim();

    if (line === '') {
      flush();
      continue;
    }

    const secondLevel = HEADING_TWO.exec(line);
    if (secondLevel !== null) {
      flush();
      blocks.push({ kind: 'heading', level: 2, text: secondLevel[1].trim() });
      continue;
    }

    const firstLevel = HEADING_ONE.exec(line);
    if (firstLevel !== null) {
      flush();
      blocks.push({ kind: 'heading', level: 1, text: firstLevel[1].trim() });
      continue;
    }

    const bullet = BULLET.exec(line);
    if (bullet !== null) {
      flush();
      blocks.push({ kind: 'bullet', runs: inlineRuns(bullet[1].trim()) });
      continue;
    }

    paragraph.push(line);
  }

  flush();

  return blocks;
}

/**
 * The blocks a legal screen draws.
 *
 * A single leading level-1 heading is dropped: it is the document's own title
 * ("Knot — Privacy Policy"), and the screen's header already names the document,
 * so drawing both would say it twice. Any other heading is kept.
 */
export function legalDocumentBlocks(source: string): readonly LegalBlock[] {
  const blocks = parseLegalMarkdown(source);
  const first = blocks[0];

  if (first !== undefined && first.kind === 'heading' && first.level === 1) {
    return blocks.slice(1);
  }

  return blocks;
}

/**
 * Splits one line of text into plain and bold runs.
 *
 * `**` toggles emphasis, so `Eat **now** please` becomes three runs. An
 * unbalanced `**` leaves the trailing run bold rather than throwing: this is
 * display text, and the worst case is a slightly emphatic sentence.
 */
function inlineRuns(text: string): readonly LegalRun[] {
  const parts = text.split('**');
  const runs: LegalRun[] = [];

  for (let index = 0; index < parts.length; index += 1) {
    const part = parts[index];
    if (part === '') {
      continue;
    }
    runs.push({ text: part, bold: index % 2 === 1 });
  }

  return runs;
}

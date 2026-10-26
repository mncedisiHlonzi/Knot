import { PRIVACY_POLICY, TERMS_OF_SERVICE } from '../../../data/legalDocuments';
import { legalDocumentBlocks, parseLegalMarkdown } from '../legalMarkdown';

/**
 * The parser is the whole renderer, so it is tested against the exact markdown the
 * founder approved as well as against small inputs that pin one rule each
 * (KNOT-035). It is pure and imports nothing from React Native.
 */
describe('parseLegalMarkdown', () => {
  it('turns a single leading title into a level-1 heading', () => {
    expect(parseLegalMarkdown('# Knot — Privacy Policy')).toEqual([
      { kind: 'heading', level: 1, text: 'Knot — Privacy Policy' },
    ]);
  });

  it('reads a section heading as level 2, not as a level-1 title', () => {
    expect(parseLegalMarkdown('## 1. What We Collect')).toEqual([
      { kind: 'heading', level: 2, text: '1. What We Collect' },
    ]);
  });

  it('joins a hard-wrapped paragraph into one block', () => {
    const blocks = parseLegalMarkdown('one line\ncontinues here\n\nsecond');

    expect(blocks).toEqual([
      {
        kind: 'paragraph',
        runs: [{ text: 'one line continues here', bold: false }],
      },
      { kind: 'paragraph', runs: [{ text: 'second', bold: false }] },
    ]);
  });

  it('splits bold runs out of a paragraph', () => {
    const blocks = parseLegalMarkdown('We do **not** collect it.');

    expect(blocks).toEqual([
      {
        kind: 'paragraph',
        runs: [
          { text: 'We do ', bold: false },
          { text: 'not', bold: true },
          { text: ' collect it.', bold: false },
        ],
      },
    ]);
  });

  it('reads a bullet, including the three-space indent the documents use', () => {
    const blocks = parseLegalMarkdown('*   **Account Information:** we keep it.');

    expect(blocks).toEqual([
      {
        kind: 'bullet',
        runs: [
          { text: 'Account Information:', bold: true },
          { text: ' we keep it.', bold: false },
        ],
      },
    ]);
  });

  it('keeps an unrecognised line as text rather than dropping it', () => {
    const blocks = parseLegalMarkdown('| a table | a future edit |');

    expect(blocks).toEqual([
      { kind: 'paragraph', runs: [{ text: '| a table | a future edit |', bold: false }] },
    ]);
  });
});

describe('legalDocumentBlocks', () => {
  it('drops the document title, because the screen header names it', () => {
    const blocks = legalDocumentBlocks('# Title\n\nFirst paragraph.');

    expect(blocks).toEqual([
      { kind: 'paragraph', runs: [{ text: 'First paragraph.', bold: false }] },
    ]);
  });

  it('keeps a heading that is not the leading title', () => {
    const blocks = legalDocumentBlocks('# Title\n\n## Section\n\nBody.');

    expect(blocks[0]).toEqual({ kind: 'heading', level: 2, text: 'Section' });
  });

  it('leaves a document that has no title untouched', () => {
    expect(legalDocumentBlocks('Only a paragraph.')).toHaveLength(1);
  });
});

describe('the approved documents', () => {
  it('renders every section of the Privacy Policy with no leftover markup', () => {
    const blocks = legalDocumentBlocks(PRIVACY_POLICY);
    const headings = blocks.flatMap((block) => (block.kind === 'heading' ? [block.text] : []));

    expect(headings).toEqual([
      '1. What We Collect',
      '2. What We Do Not Collect',
      '3. How We Use Your Data',
      '4. How We Protect Your Data',
      '5. Your Data, Your Control',
      '6. Data Sharing',
      '7. Contact Us',
    ]);

    // No paragraph or bullet may still carry the markdown that produced it.
    const text = blocks
      .flatMap((block) =>
        block.kind === 'heading' ? [block.text] : block.runs.map((run) => run.text),
      )
      .join('\n');
    expect(text).not.toContain('**');
    expect(text).not.toContain('# ');
  });

  it('renders every section of the Terms of Service with no leftover markup', () => {
    const blocks = legalDocumentBlocks(TERMS_OF_SERVICE);
    const headings = blocks.flatMap((block) => (block.kind === 'heading' ? [block.text] : []));

    expect(headings).toEqual([
      '1. Who Can Use Knot',
      '2. What You Can Share',
      '3. Adaptations (Tell My People)',
      '4. Respectful Disagreement',
      '5. Safety and Moderation',
      '6. Account Deletion',
      '7. Changes to These Terms',
      '8. Disclaimer',
      '9. Contact',
    ]);
  });

  it('keeps the bold emphasis the documents rely on', () => {
    const blocks = legalDocumentBlocks(TERMS_OF_SERVICE);
    const boldRuns = blocks.flatMap((block) =>
      block.kind === 'heading' ? [] : block.runs.filter((run) => run.bold).map((run) => run.text),
    );

    expect(boldRuns.length).toBeGreaterThan(0);
  });
});

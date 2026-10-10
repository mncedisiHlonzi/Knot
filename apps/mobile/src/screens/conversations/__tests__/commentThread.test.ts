import type { Comment } from '../../../api/conversations';
import { LANGUAGES } from '../../../data/languages';
import {
  DEFAULT_COMPOSE_LANGUAGE,
  MAX_CHIP_LENGTH,
  REPLIES_PREVIEW_COUNT,
  groupComments,
  initialComposeLanguage,
  isReply,
  languageChipLabel,
  repliesLinkLabel,
  replyTargetId,
} from '../commentThread';

/**
 * A minimal comment for the grouping tests. Only the fields the grouping reads are
 * meaningful; the rest are fixed so the object satisfies the type.
 */
function makeComment(id: string, parentCommentId: string | null): Comment {
  return {
    id,
    version_id: 'version-1',
    author_id: `author-${id}`,
    language: 'eng',
    body: `body of ${id}`,
    parent_comment_id: parentCommentId,
    created_at: '2026-10-10T00:00:00Z',
    updated_at: '2026-10-10T00:00:00Z',
  };
}

describe('replyTargetId', () => {
  it('targets the comment itself when it is top level', () => {
    expect(replyTargetId({ id: 'top', parent_comment_id: null })).toBe('top');
  });

  it('targets the top-level comment when replying to a reply', () => {
    // Threading is one level deep: a reply to a reply stays in the same thread,
    // which is what the server stores too (KNOT-ADR-047).
    expect(replyTargetId({ id: 'reply', parent_comment_id: 'top' })).toBe('top');
  });
});

describe('isReply', () => {
  it('reports a comment with no parent as top level', () => {
    expect(isReply({ parent_comment_id: null })).toBe(false);
  });

  it('reports a comment with a parent as a reply', () => {
    expect(isReply({ parent_comment_id: 'top' })).toBe(true);
  });
});

describe('groupComments', () => {
  it('returns empty structures for an empty page', () => {
    expect(groupComments([])).toEqual({ topLevel: [], repliesByParent: {} });
  });

  it('keeps only top-level comments when there are no replies', () => {
    const grouped = groupComments([
      makeComment('a', null),
      makeComment('b', null),
      makeComment('c', null),
    ]);

    expect(grouped.topLevel.map((comment) => comment.id)).toEqual(['a', 'b', 'c']);
    expect(grouped.repliesByParent).toEqual({});
  });

  it('groups replies under their parent, preserving every row order', () => {
    // The API already returns parent, its replies, next parent, its replies.
    const grouped = groupComments([
      makeComment('a', null),
      makeComment('a1', 'a'),
      makeComment('a2', 'a'),
      makeComment('b', null),
      makeComment('b1', 'b'),
    ]);

    expect(grouped.topLevel.map((comment) => comment.id)).toEqual(['a', 'b']);
    expect(grouped.repliesByParent.a.map((comment) => comment.id)).toEqual(['a1', 'a2']);
    expect(grouped.repliesByParent.b.map((comment) => comment.id)).toEqual(['b1']);
  });

  it('has no entry for a top-level comment with no replies', () => {
    const grouped = groupComments([makeComment('a', null), makeComment('b', null)]);

    expect(grouped.repliesByParent.a).toBeUndefined();
  });

  it('does not drop a reply whose parent is not in the page', () => {
    // The parent may be on a later page; the reply is still grouped under it.
    const grouped = groupComments([makeComment('orphan', 'missing-parent')]);

    expect(grouped.topLevel).toEqual([]);
    expect(grouped.repliesByParent['missing-parent'].map((comment) => comment.id)).toEqual([
      'orphan',
    ]);
  });

  it('treats a missing parent id as top level, because a response can omit it', () => {
    const withoutParent = { ...makeComment('a', null) };
    delete (withoutParent as { parent_comment_id?: string | null }).parent_comment_id;

    const grouped = groupComments([withoutParent as Comment]);

    expect(grouped.topLevel.map((comment) => comment.id)).toEqual(['a']);
  });
});

describe('repliesLinkLabel', () => {
  it('has no link when there are no replies', () => {
    expect(repliesLinkLabel(0, false, false)).toBeNull();
    expect(repliesLinkLabel(0, true, false)).toBeNull();
    expect(repliesLinkLabel(0, true, true)).toBeNull();
    expect(repliesLinkLabel(-1, false, false)).toBeNull();
  });

  it('counts a single reply in the singular', () => {
    expect(repliesLinkLabel(1, false, false)).toBe('View 1 reply');
  });

  it('offers the replies without "all" for two or three', () => {
    expect(repliesLinkLabel(2, false, false)).toBe('View 2 replies');
    expect(repliesLinkLabel(3, false, false)).toBe('View 3 replies');
  });

  it('offers all replies when more than the preview is collapsed', () => {
    expect(REPLIES_PREVIEW_COUNT).toBe(3);
    expect(repliesLinkLabel(4, false, false)).toBe('View all 4 replies');
    expect(repliesLinkLabel(100, false, false)).toBe('View all 100 replies');
  });

  it('hides replies once they are all shown by the first tap', () => {
    // Three or fewer are fully revealed on the first tap, so the link flips to Hide.
    expect(repliesLinkLabel(1, true, false)).toBe('Hide replies');
    expect(repliesLinkLabel(2, true, false)).toBe('Hide replies');
    expect(repliesLinkLabel(3, true, false)).toBe('Hide replies');
  });

  it('offers the rest while expanded with more than the preview hidden', () => {
    expect(repliesLinkLabel(4, true, false)).toBe('View all 4 replies');
    expect(repliesLinkLabel(100, true, false)).toBe('View all 100 replies');
  });

  it('hides replies once the rest are revealed on the second tap', () => {
    expect(repliesLinkLabel(4, true, true)).toBe('Hide replies');
    expect(repliesLinkLabel(100, true, true)).toBe('Hide replies');
  });

  it('ignores a stale fully-expanded flag while collapsed', () => {
    // Defensive: a hidden comment is described by its collapsed label regardless
    // of a leftover fully-expanded flag.
    expect(repliesLinkLabel(4, false, true)).toBe('View all 4 replies');
    expect(repliesLinkLabel(1, false, true)).toBe('View 1 reply');
  });
});

describe('languageChipLabel', () => {
  it('shows the name when it fits the chip', () => {
    expect(languageChipLabel('eng')).toBe('English');
    expect(languageChipLabel('zul')).toBe('Zulu');
  });

  it('keeps a name that is exactly the limit', () => {
    // "Abellen Ayta" is 12 characters, the widest name the chip still shows.
    expect('Abellen Ayta'.length).toBe(MAX_CHIP_LENGTH);
    expect(languageChipLabel('abp')).toBe('Abellen Ayta');
  });

  it('shows the code when the name is too long', () => {
    expect(languageChipLabel('tpx')).toBe('tpx');
  });

  it('falls back to an unknown code, because the server may know more than the app', () => {
    expect(languageChipLabel('xyz')).toBe('xyz');
  });

  it('has no label for an empty code, so the chip shows its placeholder', () => {
    expect(languageChipLabel('')).toBe('');
    expect(languageChipLabel('   ')).toBe('');
  });

  it('labels every canonical language within the chip width', () => {
    for (const language of LANGUAGES) {
      const label = languageChipLabel(language.code);
      expect(label.length).toBeLessThanOrEqual(MAX_CHIP_LENGTH);
      expect(label).toBe(language.name.length <= MAX_CHIP_LENGTH ? language.name : language.code);
    }
  });

  it('actually exercises the fallback, so the rule is not vacuous', () => {
    const tooLong = LANGUAGES.filter((language) => language.name.length > MAX_CHIP_LENGTH);
    expect(tooLong.length).toBeGreaterThan(0);
  });
});

describe('initialComposeLanguage', () => {
  it('uses the first preferred language when the app knows it', () => {
    expect(initialComposeLanguage('zul', 'eng')).toBe('zul');
  });

  it('falls back to the version language when the user has no preference', () => {
    expect(initialComposeLanguage(undefined, 'fra')).toBe('fra');
    expect(initialComposeLanguage('', 'fra')).toBe('fra');
  });

  it('ignores a preference the app does not know, e.g. a two-letter tag', () => {
    expect(initialComposeLanguage('zu', 'eng')).toBe('eng');
    expect(initialComposeLanguage('en')).toBe(DEFAULT_COMPOSE_LANGUAGE);
  });

  it('falls back to English when neither is usable', () => {
    expect(initialComposeLanguage(undefined, undefined)).toBe(DEFAULT_COMPOSE_LANGUAGE);
    expect(initialComposeLanguage('  ', '  ')).toBe(DEFAULT_COMPOSE_LANGUAGE);
  });
});

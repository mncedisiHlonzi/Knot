import type { StoryVersion } from '../../../api/versions';
import { adaptCountLabel, treeRows, treeSummary } from '../versionTree';

/** A version fixture: only the fields the tree reads matter. */
function version(
  id: string,
  parent: string | null,
  language = 'eng',
  title = 'The first rain',
): StoryVersion {
  return {
    id,
    story_id: 'story-1',
    parent_version_id: parent,
    author_id: 'author-1',
    language,
    title,
    body: 'body',
    adaptation_note: null,
    created_at: '2026-10-09T12:00:00.000Z',
    updated_at: '2026-10-09T12:00:00.000Z',
  };
}

const ROOT = 'root';
const CHILD_A = 'child-a';
const CHILD_B = 'child-b';
const GRANDCHILD = 'grandchild';

// root ─ child-a ─ grandchild
//      └ child-b
const TREE: readonly StoryVersion[] = [
  version(ROOT, null, 'zap', 'How my grandmother counted the days'),
  version(CHILD_A, ROOT, 'spa'),
  version(GRANDCHILD, CHILD_A, 'mix'),
  version(CHILD_B, ROOT, 'eng'),
];

describe('treeRows', () => {
  it('walks the tree depth-first, parents above their children', () => {
    expect(treeRows(TREE).map((row) => [row.version.id, row.depth])).toEqual([
      [ROOT, 0],
      [CHILD_A, 1],
      [GRANDCHILD, 2],
      [CHILD_B, 1],
    ]);
  });

  it('counts each version\u2019s direct adaptations', () => {
    const counts = new Map(treeRows(TREE).map((row) => [row.version.id, row.adaptCount]));

    expect(counts.get(ROOT)).toBe(2);
    expect(counts.get(CHILD_A)).toBe(1);
    expect(counts.get(GRANDCHILD)).toBe(0);
  });

  it('treats a version whose parent is missing as a root', () => {
    const orphaned = [version('orphan', 'not-in-the-list')];
    expect(treeRows(orphaned).map((row) => [row.version.id, row.depth])).toEqual([['orphan', 0]]);
  });

  it('never hangs on a parent cycle, and still renders every version', () => {
    const cyclic = [version('a', 'b'), version('b', 'a')];
    const rows = treeRows(cyclic);

    expect(rows).toHaveLength(2);
    expect(new Set(rows.map((row) => row.version.id))).toEqual(new Set(['a', 'b']));
  });

  it('renders nothing for an empty tree', () => {
    expect(treeRows([])).toEqual([]);
  });
});

describe('treeSummary', () => {
  it('names the story after its root and counts versions and languages', () => {
    expect(treeSummary(treeRows(TREE))).toEqual({
      title: 'How my grandmother counted the days',
      versionCount: 4,
      languageCount: 4,
    });
  });

  it('is empty for an empty tree', () => {
    expect(treeSummary([])).toEqual({ title: '', versionCount: 0, languageCount: 0 });
  });
});

describe('adaptCountLabel', () => {
  it('pluralises', () => {
    expect(adaptCountLabel(0)).toBe('0 adaptations');
    expect(adaptCountLabel(1)).toBe('1 adaptation');
    expect(adaptCountLabel(4)).toBe('4 adaptations');
  });
});
